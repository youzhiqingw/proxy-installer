// Package singbox 负责 sing-box 二进制管理：本机查找/下载、远端上传安装、SHA256 校验。
// 从 deploy/singbox.go 提取为独立包，避免 deploy 包过大。
package singbox

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"proxy-installer/internal/sshclient"
	"proxy-installer/internal/util"
)

type githubRelease struct {
	Assets []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// EnsureLocalSingBox 查找或下载本机 sing-box 可执行文件路径
func EnsureLocalSingBox() (string, error) {
	if bin, err := findLocalSingBox(); err == nil {
		return bin, nil
	}
	bin, err := downloadLocalSingBox()
	if err != nil {
		return "", fmt.Errorf("本机未找到 sing-box，自动下载也失败: %w", err)
	}
	return bin, nil
}

// InstallSingBoxViaUpload 检测远端架构，下载对应 Linux sing-box 并上传到远端
func InstallSingBoxViaUpload(client *ssh.Client, emit sshclient.EmitFn) error {
	arch, err := detectRemoteSingBoxArch(client)
	if err != nil {
		return err
	}
	emit("log", 34, "远端架构: linux-"+arch)
	localPath, err := DownloadLinuxSingBox(arch)
	if err != nil {
		return err
	}
	emit("log", 34, "本机已准备 sing-box: "+localPath)
	if err := uploadSingBoxBinary(client, localPath); err != nil {
		return err
	}
	emit("log", 34, "已上传 sing-box 到 /usr/local/bin/sing-box")
	return nil
}

func detectRemoteSingBoxArch(client *ssh.Client) (string, error) {
	result, err := sshclient.RunCommand(client, `uname -s; uname -m`, 10*time.Second)
	if err != nil {
		return "", err
	}
	lines := strings.Fields(strings.ToLower(result.Stdout))
	if len(lines) < 2 || lines[0] != "linux" {
		return "", fmt.Errorf("远端系统不是 Linux 或无法识别: %s", strings.TrimSpace(result.Stdout))
	}
	switch lines[1] {
	case "x86_64", "amd64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	case "armv7l", "armv7":
		return "armv7", nil
	default:
		return "", fmt.Errorf("暂不支持远端架构: %s", lines[1])
	}
}

// DownloadLinuxSingBox 下载指定架构的 Linux sing-box 二进制到本地缓存
func DownloadLinuxSingBox(arch string) (string, error) {
	dir, err := localSingBoxDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "linux-"+arch)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	target := filepath.Join(dir, "sing-box")
	if stat, err := os.Stat(target); err == nil && !stat.IsDir() && stat.Size() > 1024*1024 {
		return target, nil
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/SagerNet/sing-box/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ProxyInstaller/1.0")
	client := http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("本机访问 GitHub Release API 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("本机 GitHub Release API HTTP %d", resp.StatusCode)
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4*1024*1024)).Decode(&release); err != nil {
		return "", err
	}
	want := fmt.Sprintf("linux-%s.tar.gz", arch)
	assetURL := ""
	var checksumURL string
	for _, asset := range release.Assets {
		name := strings.ToLower(asset.Name)
		if strings.HasSuffix(name, want) && !strings.Contains(name, "legacy") && asset.BrowserDownloadURL != "" {
			assetURL = asset.BrowserDownloadURL
		}
		if name == "sha256sum.txt" && asset.BrowserDownloadURL != "" {
			checksumURL = asset.BrowserDownloadURL
		}
		if assetURL != "" && checksumURL != "" {
			break
		}
	}
	if assetURL == "" {
		return "", fmt.Errorf("未找到 sing-box Linux %s release 资源", arch)
	}
	archivePath := filepath.Join(dir, "sing-box-linux-"+arch+".tar.gz")
	if err := downloadFile(archivePath, assetURL, 120*1024*1024); err != nil {
		return "", err
	}
	defer os.Remove(archivePath)

	// SHA256 校验：下载 sha256sum.txt 并验证压缩包完整性
	if checksumURL != "" {
		if err := VerifySHASums(archivePath, checksumURL); err != nil {
			return "", fmt.Errorf("SHA256 校验失败: %w （若持续失败可跳过校验手动安装 sing-box）", err)
		}
	}

	if err := extractSingBoxTarGz(archivePath, target); err != nil {
		return "", err
	}
	return target, nil
}

func uploadSingBoxBinary(client *ssh.Client, localPath string) error {
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return err
	}
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	script := `
set -e
tmp="$(mktemp /tmp/proxy-installer-singbox-upload.XXXXXX)"
base64 -d > "$tmp"
chmod 755 "$tmp"
mkdir -p /usr/local/bin
mv "$tmp" /usr/local/bin/sing-box
/usr/local/bin/sing-box version
`
	if err := session.Start("bash -lc " + util.ShellQuote(script)); err != nil {
		return err
	}

	copyDone := make(chan error, 1)
	go func() {
		encoder := base64.NewEncoder(base64.StdEncoding, stdin)
		_, copyErr := io.Copy(encoder, file)
		closeErr := encoder.Close()
		stdinCloseErr := stdin.Close()
		if copyErr != nil {
			copyDone <- copyErr
			return
		}
		if closeErr != nil {
			copyDone <- closeErr
			return
		}
		copyDone <- stdinCloseErr
	}()

	waitDone := make(chan error, 1)
	go func() { waitDone <- session.Wait() }()

	var waitErr error
	select {
	case waitErr = <-waitDone:
	case <-time.After(4 * time.Minute):
		_ = session.Signal(ssh.SIGKILL)
		return fmt.Errorf("上传 sing-box 超时")
	}
	copyErr := <-copyDone
	if waitErr != nil {
		return fmt.Errorf("远端安装上传的 sing-box 失败: %w stdout=%s stderr=%s", waitErr, util.TrimForMessage(stdout.String(), 400), util.TrimForMessage(stderr.String(), 400))
	}
	if copyErr != nil {
		return fmt.Errorf("上传 sing-box 数据失败: %w", copyErr)
	}
	return nil
}

func downloadLocalSingBox() (string, error) {
	if goruntime.GOOS != "windows" {
		return "", fmt.Errorf("当前系统暂不支持自动下载，请把 sing-box 放入 PATH")
	}
	arch := goruntime.GOARCH
	if arch != "amd64" && arch != "arm64" {
		return "", fmt.Errorf("当前架构 %s 暂不支持自动下载", arch)
	}
	dir, err := localSingBoxDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	target := filepath.Join(dir, "sing-box.exe")
	if stat, err := os.Stat(target); err == nil && !stat.IsDir() {
		return target, nil
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/SagerNet/sing-box/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ProxyInstaller/1.0")
	client := http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("GitHub release API HTTP %d", resp.StatusCode)
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4*1024*1024)).Decode(&release); err != nil {
		return "", err
	}
	assetURL := ""
	want := fmt.Sprintf("windows-%s.zip", arch)
	for _, asset := range release.Assets {
		name := strings.ToLower(asset.Name)
		if strings.HasSuffix(name, want) && !strings.Contains(name, "legacy") && asset.BrowserDownloadURL != "" {
			assetURL = asset.BrowserDownloadURL
			break
		}
	}
	if assetURL == "" {
		return "", fmt.Errorf("未找到 sing-box Windows %s release 资产", arch)
	}
	zipPath := filepath.Join(dir, "sing-box-latest.zip")
	if err := downloadFile(zipPath, assetURL, 120*1024*1024); err != nil {
		return "", err
	}
	defer os.Remove(zipPath)
	if err := extractSingBoxZip(zipPath, target); err != nil {
		return "", err
	}
	return target, nil
}

// VerifySHASums 从 checksumURL 下载 sha256sum.txt 并验证 archivePath 的完整性
func VerifySHASums(archivePath, checksumURL string) error {
	archiveName := filepath.Base(archivePath)

	req, err := http.NewRequest(http.MethodGet, checksumURL, nil)
	if err != nil {
		return fmt.Errorf("创建校验和请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "ProxyInstaller/1.0")
	client := http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("下载 sha256sum.txt 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("sha256sum.txt HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return fmt.Errorf("读取 sha256sum.txt 失败: %w", err)
	}

	// 解析 sha256sum.txt，格式: "<sha256>  <filename>"
	wantChecksum := ""
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, archiveName) {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				wantChecksum = parts[0]
				break
			}
		}
	}
	if wantChecksum == "" {
		return fmt.Errorf("sha256sum.txt 中未找到 %s 的校验值", archiveName)
	}

	// 计算本地文件 SHA256
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("打开文件计算 SHA256 失败: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("计算 SHA256 失败: %w", err)
	}
	gotChecksum := hex.EncodeToString(h.Sum(nil))

	if !strings.EqualFold(gotChecksum, wantChecksum) {
		return fmt.Errorf("SHA256 不匹配: 期望 %s, 实际 %s", wantChecksum, gotChecksum)
	}
	return nil
}

func localSingBoxDir() (string, error) {
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		return filepath.Join(local, "proxy-installer", "runtime"), nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "proxy-installer", "runtime"), nil
}

func downloadFile(path, rawURL string, maxBytes int64) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ProxyInstaller/1.0")
	client := http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("下载 sing-box HTTP %d", resp.StatusCode)
	}
	tmp := path + ".download"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(file, io.LimitReader(resp.Body, maxBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if n > maxBytes {
		_ = os.Remove(tmp)
		return fmt.Errorf("下载文件超过大小限制 %d bytes", maxBytes)
	}
	return os.Rename(tmp, path)
}

func extractSingBoxZip(zipPath, target string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if !strings.HasSuffix(f.Name, "sing-box.exe") && !strings.HasSuffix(f.Name, "sing-box") {
			continue
		}
		if f.FileInfo().Mode()&0111 == 0 && !strings.HasSuffix(f.Name, ".exe") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(rc, 200*1024*1024))
		rc.Close()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0755)
	}
	return fmt.Errorf("zip 中未找到 sing-box 可执行文件")
}

func extractSingBoxTarGz(archivePath, target string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := filepath.Base(hdr.Name)
		if name != "sing-box" {
			continue
		}
		if hdr.FileInfo().Mode()&0111 == 0 {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, 200*1024*1024))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0755)
	}
	return fmt.Errorf("tar.gz 中未找到 sing-box 可执行文件")
}

func findLocalSingBox() (string, error) {
	if bin, err := exec.LookPath("sing-box"); err == nil {
		return bin, nil
	}
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(dir, "sing-box.exe"), filepath.Join(dir, "sing-box"))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "sing-box.exe"),
			filepath.Join(wd, "build", "bin", "sing-box.exe"),
			filepath.Join(wd, "build", "bin", "sing-box"),
		)
	}
	if dir, err := localSingBoxDir(); err == nil {
		candidates = append(candidates, filepath.Join(dir, "sing-box.exe"))
	}
	candidates = append(candidates,
		`C:\Program Files\sing-box\sing-box.exe`,
		`C:\Program Files (x86)\sing-box\sing-box.exe`,
	)
	for _, item := range candidates {
		if stat, err := os.Stat(item); err == nil && !stat.IsDir() {
			return item, nil
		}
	}
	return "", fmt.Errorf("本机未找到 sing-box，请把 sing-box.exe 放到程序同目录或加入 PATH 后再测试节点速度")
}

