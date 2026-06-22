// Package sshclient 封装 SSH 连接、HostKey 管理和远程命令执行。
// 从 app.go 提取，不依赖 Wails runtime（通过 EmitFn 回调解耦）。
package sshclient

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"proxy-installer/internal/config"
	apperr "proxy-installer/internal/errors"
	"proxy-installer/internal/logger"
)

// EmitFn 是部署事件回调函数，由调用方注入（通常绑定 Wails runtime.EventsEmit）
type EmitFn func(kind string, percent int, message string)

// ── HostKey 管理 ──────────────────────────────────────────

// PendingHostKey 暂存首次连接时的 HostKey 信息，等待用户确认
type PendingHostKey struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	KeyType     string `json:"keyType"`
	Fingerprint string `json:"fingerprint"`
	KeyBytes    []byte `json:"-"`
}

// ErrNewHostKey 表示首次连接遇到未知 HostKey，需要用户确认
type ErrNewHostKey struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	KeyType     string `json:"keyType"`
	Fingerprint string `json:"fingerprint"`
}

func (e *ErrNewHostKey) Error() string {
	return fmt.Sprintf("HOSTKEY_CONFIRM:%s:%d 首次连接，请确认服务器指纹 %s (%s)", e.Host, e.Port, e.Fingerprint, e.KeyType)
}

// HostKeyEntry 用于存储已知 HostKey
type HostKeyEntry struct {
	Host  string `json:"host"`
	Port  int    `json:"port"`
	Keys  []byte `json:"keys"`
	Hash  string `json:"hash"`
	Added string `json:"added"`
}

// HostKeyStore 管理已知 HostKey
type HostKeyStore struct {
	entries []HostKeyEntry
	mu      sync.RWMutex
	path    string
}

// NewHostKeyStore 创建 HostKey 存储并从文件加载
func NewHostKeyStore(path string) (*HostKeyStore, error) {
	store := &HostKeyStore{path: path}
	if err := store.Load(); err != nil {
		return nil, err
	}
	return store, nil
}

// Load 从 JSON 文件加载 HostKey 条目
func (s *HostKeyStore) Load() error {
	data, err := readFileIfExists(s.path)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Unmarshal(data, &s.entries)
}

// Save 将 HostKey 条目持久化到 JSON 文件（线程安全）
func (s *HostKeyStore) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.save()
}

// save 内部写入方法，调用者必须已持有锁
func (s *HostKeyStore) save() error {
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.path, data)
}

// Get 查找指定主机和端口的已存储 HostKey
func (s *HostKeyStore) Get(host string, port int) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, e := range s.entries {
		if e.Host == host && e.Port == port {
			return e.Keys, true
		}
	}
	return nil, false
}

// Add 添加或更新指定主机的 HostKey
func (s *HostKeyStore) Add(host string, port int, keys []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.entries {
		if e.Host == host && e.Port == port {
			s.entries[i].Keys = keys
			s.entries[i].Added = time.Now().Format(time.RFC3339)
			return s.save()
		}
	}
	s.entries = append(s.entries, HostKeyEntry{
		Host:  host,
		Port:  port,
		Keys:  keys,
		Hash:  fmt.Sprintf("sha256:%s", hex.EncodeToString(keys)),
		Added: time.Now().Format(time.RFC3339),
	})
	return s.save()
}

// Remove 删除指定主机的 HostKey
func (s *HostKeyStore) Remove(host string, port int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.entries {
		if e.Host == host && e.Port == port {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			_ = s.save()
			return true
		}
	}
	return false
}

// Entries 返回所有已存储的 HostKey 条目
func (s *HostKeyStore) Entries() []HostKeyEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.entries
}

// ── SSH Client ──────────────────────────────────────────

// Client 封装 SSH 连接和 HostKey 管理逻辑
type Client struct {
	Store        *HostKeyStore
	mu           sync.Mutex
	pendingKey   *PendingHostKey
	normalizeHost func(string) string // 可选：注入自定义的 host 标准化函数
}

// NewClient 创建 SSH Client 实例
func NewClient(store *HostKeyStore) *Client {
	return &Client{
		Store:         store,
		normalizeHost: NormalizeHost,
	}
}

// Connect 建立 SSH 连接，成功后返回 *ssh.Client
func (c *Client) Connect(profile config.SSHProfile) (*ssh.Client, error) {
	host := c.normalizeHost(profile.Host)
	if host == "" {
		return nil, fmt.Errorf("请输入 VPS 主机/IP")
	}
	user := strings.TrimSpace(profile.User)
	if user == "" {
		user = strings.TrimSpace(profile.Username)
	}
	if user == "" {
		user = "root"
	}
	port := profile.Port
	if port == 0 {
		port = 22
	}
	if len(profile.Password) == 0 {
		return nil, fmt.Errorf("请输入 SSH 密码")
	}

	// 将密码转为字符串用于 SSH 认证，立即将字节置零
	pwStr := string(profile.Password)
	profile.ClearPassword()

	cfg := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{ssh.Password(pwStr)},
		HostKeyCallback: func(h string, remote net.Addr, key ssh.PublicKey) error {
			return c.HostKeyCallback(h, remote, key)
		},
		Timeout: 18 * time.Second,
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	logger.Info("SSH 连接", "host", host, "port", port, "user", user)
	client, err := ssh.Dial("tcp", addr, cfg)
	// 无论连接成功与否，立即清除密码字符串
	pwStr = ""
	if err != nil {
		logger.Error("SSH 连接失败", "host", host, "port", port, "error", err.Error())
		return nil, err
	}
	logger.Info("SSH 连接成功", "host", host, "port", port)
	return client, nil
}

// HostKeyCallback 实现 SSH HostKey 验证（TOFU 模型）
func (c *Client) HostKeyCallback(host string, remote net.Addr, key ssh.PublicKey) error {
	if c.Store == nil {
		logger.Warn("HostKeyStore 未初始化，跳过验证", "host", host)
		return nil
	}
	port := remote.(*net.TCPAddr).Port
	storedKeys, found := c.Store.Get(host, port)
	if found {
		expected := string(storedKeys)
		actual := string(key.Marshal())
		if expected == actual {
			return nil
		}
		logger.Warn("SSH HostKey 变更", "host", host, "port", port,
			"fingerprint", ssh.FingerprintSHA256(key))
		return fmt.Errorf("SSH HostKey 变更，可能存在中间人攻击风险")
	}
	// 首次连接：暂存 HostKey 信息，返回错误让前端弹出确认
	fp := ssh.FingerprintSHA256(key)
	logger.Info("首次连接，等待用户确认 HostKey", "host", host, "port", port, "fingerprint", fp)
	c.mu.Lock()
	c.pendingKey = &PendingHostKey{
		Host:        host,
		Port:        port,
		KeyType:     key.Type(),
		Fingerprint: fp,
		KeyBytes:    key.Marshal(),
	}
	c.mu.Unlock()
	return &ErrNewHostKey{Host: host, Port: port, KeyType: key.Type(), Fingerprint: fp}
}

// AcceptHostKey 接受并存储待确认的 HostKey
func (c *Client) AcceptHostKey(host string, port int) error {
	c.mu.Lock()
	pending := c.pendingKey
	c.pendingKey = nil
	c.mu.Unlock()
	if pending == nil {
		return fmt.Errorf("没有待确认的 HostKey")
	}
	if pending.Host != host || pending.Port != port {
		return fmt.Errorf("HostKey 信息不匹配")
	}
	if c.Store == nil {
		return fmt.Errorf("HostKeyStore 未初始化")
	}
	if err := c.Store.Add(host, port, pending.KeyBytes); err != nil {
		logger.Error("保存 HostKey 失败", "host", host, "error", err.Error())
		return err
	}
	logger.Info("用户已确认 HostKey", "host", host, "port", port, "fingerprint", pending.Fingerprint)
	return nil
}

// ── 远程命令执行 ──────────────────────────────────────────

// RunCommand 在远程执行命令，等待完成并返回结果
func RunCommand(client *ssh.Client, command string, timeout time.Duration) (config.CommandResult, error) {
	session, err := client.NewSession()
	if err != nil {
		return config.CommandResult{}, apperr.NewSSH("创建 SSH 会话失败", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()

	select {
	case err := <-done:
		code := 0
		if err != nil {
			if exitErr, ok := err.(*ssh.ExitError); ok {
				code = exitErr.ExitStatus()
			} else {
				return config.CommandResult{}, apperr.NewSSH("执行远程命令失败", err)
			}
		}
		return config.CommandResult{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}, nil
	case <-time.After(timeout):
		_ = session.Signal(ssh.SIGKILL)
		return config.CommandResult{}, apperr.NewSSH("远程命令超时", nil)
	}
}

// RunStreaming 在远程执行命令并流式读取输出，通过 emitFn 回调推送事件
func RunStreaming(client *ssh.Client, command string, emitFn EmitFn) (int, error) {
	session, err := client.NewSession()
	if err != nil {
		return -1, apperr.NewSSH("创建 SSH 会话失败", err)
	}
	defer session.Close()

	stdout, err := session.StdoutPipe()
	if err != nil {
		return -1, apperr.NewSSH("获取标准输出管道失败", err)
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return -1, apperr.NewSSH("获取标准错误管道失败", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go scanDeployStream(stdout, "log", &wg, emitFn)
	go scanDeployStream(stderr, "log", &wg, emitFn)

	if err := session.Start(command); err != nil {
		return -1, apperr.NewSSH("启动远程命令失败", err)
	}
	err = session.Wait()
	wg.Wait()

	if err == nil {
		return 0, nil
	}
	if exitErr, ok := err.(*ssh.ExitError); ok {
		return exitErr.ExitStatus(), nil
	}
	return -1, apperr.NewSSH("等待远程命令完成失败", err)
}

// ── 工具函数 ──────────────────────────────────────────

// NormalizeHost 标准化主机地址（去除协议前缀、方括号等）
func NormalizeHost(host string) string {
	host = strings.TrimSpace(host)
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		if parsed, err := url.Parse(host); err == nil && parsed.Hostname() != "" {
			host = parsed.Hostname()
		}
	}
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	return host
}

// scanDeployStream 逐行扫描远程命令输出，解析部署事件并通过 emitFn 推送
func scanDeployStream(reader io.Reader, fallbackType string, wg *sync.WaitGroup, emitFn EmitFn) {
	defer wg.Done()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "__VPS_STARTER_EVENT__|") {
			parts := strings.SplitN(line, "|", 4)
			if len(parts) == 4 {
				percent, _ := strconv.Atoi(parts[2])
				messageBytes, err := base64.StdEncoding.DecodeString(parts[3])
				if err == nil {
					emitFn(parts[1], percent, string(messageBytes))
					continue
				}
			}
		}
		var event config.DeployEvent
		if err := json.Unmarshal([]byte(line), &event); err == nil && event.Type != "" {
			emitFn(event.Type, event.Percent, event.Message)
			continue
		}
		emitFn(fallbackType, 0, line)
	}
}

// ── 文件 IO 辅助 ──────────────────────────────────────────

func readFileIfExists(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
