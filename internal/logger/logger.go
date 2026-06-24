// Package logger 提供应用级结构化日志功能。
// 日志写入 %LOCALAPPDATA%/proxy-installer/logs/ 目录，按日期轮转，自动清理过期文件。
package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	global     *slog.Logger
	logDir     string
	mu         sync.Mutex
	currentDay string
	file       *os.File
	retention  = 30 // 日志保留天数
)

// Init 初始化日志系统，日志文件存放在 logDir 目录下。
func Init(dir string) error {
	mu.Lock()
	defer mu.Unlock()

	logDir = filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return fmt.Errorf("创建日志目录失败: %w", err)
	}

	if err := openTodayFile(); err != nil {
		return err
	}
	cleanOldLogs()
	return nil
}

// openTodayFile 打开或创建当天的日志文件（调用前须持有 mu）。
func openTodayFile() error {
	day := time.Now().Format("2006-01-02")
	if day == currentDay && file != nil {
		return nil
	}
	if file != nil {
		file.Close()
	}
	path := filepath.Join(logDir, day+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("打开日志文件失败: %w", err)
	}
	file = f
	currentDay = day

	// 同时输出到 stdout 和日志文件
	w := io.MultiWriter(os.Stdout, file)
	handler := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	global = slog.New(handler)
	return nil
}

// rotate 检查日期并在跨天时切换到新文件。
func rotate() {
	day := time.Now().Format("2006-01-02")
	if day != currentDay {
		_ = openTodayFile()
	}
}

// Get 返回全局 slog.Logger。如果尚未初始化，返回写入 stdout 的默认 logger。
func Get() *slog.Logger {
	mu.Lock()
	defer mu.Unlock()
	if global == nil {
		global = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	rotate()
	return global
}

// 便捷函数 --------------------------------------------------------

func Debug(msg string, args ...any) { Get().Debug(msg, args...) }
func Info(msg string, args ...any)  { Get().Info(msg, args...) }
func Warn(msg string, args ...any)  { Get().Warn(msg, args...) }
func Error(msg string, args ...any) { Get().Error(msg, args...) }

// Close 关闭日志文件（应在程序退出前调用）。
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if file != nil {
		file.Close()
		file = nil
	}
}

// cleanOldLogs 删除超过保留天数的日志文件。
func cleanOldLogs() {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -retention).Format("2006-01-02")
	var oldFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".log") {
			continue
		}
		day := strings.TrimSuffix(name, ".log")
		if day < cutoff {
			oldFiles = append(oldFiles, filepath.Join(logDir, name))
		}
	}
	// 按文件名排序（日期升序），从最早的开始删除
	sort.Strings(oldFiles)
	for _, p := range oldFiles {
		_ = os.Remove(p)
	}
}
