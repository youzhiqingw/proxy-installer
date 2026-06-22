//go:build integration

// Integration tests for the deploy package.
//
// These tests require a real Docker environment with SSH access to validate
// the full deployment flow end-to-end. They are gated behind the "integration"
// build tag so that `go test ./...` will skip them by default.
//
// To run:
//
//	go test -tags integration -v ./internal/deploy/ -run TestDeploy
//
// Prerequisites:
//   - Docker daemon running
//   - TEST_SSH_HOST, TEST_SSH_USER, TEST_SSH_PASSWORD environment variables set
//     (or a Docker container spun up by a test harness)

package deploy

import (
	"testing"

	"proxy-installer/internal/config"
)

// ---------------------------------------------------------------------------
// TestDeploy_FullFlow
// ---------------------------------------------------------------------------
//
// This test would:
//  1. Spin up a Docker container running a minimal Debian/Ubuntu image with
//     an SSH server (e.g. linuxserver/openssh-server).
//  2. Establish an SSH connection to the container using sshclient.Dial.
//  3. Call Deploy() with a test SSHProfile and DeployConfig.
//  4. Verify that:
//     - sing-box is installed and running inside the container
//     - /etc/sing-box/config.json is valid JSON with expected inbounds
//     - nginx is running and serving subscription files on the web port
//     - Subscription URLs return valid client configs
//     - The Deploy() return map has ok=true and code=0
//  5. Tear down the Docker container.

func TestDeploy_FullFlow(t *testing.T) {
	t.Skip("requires Docker environment")

	// Skeleton: demonstrates the intended test structure.
	profile := config.SSHProfile{
		ID:   "integration-test",
		Name: "docker-vps",
		Host: "127.0.0.1", // Docker container mapped to localhost
		User: "root",
		Port: 2222, // Host-mapped SSH port
	}
	cfg := config.DeployConfig{
		ProfileID: "integration-test",
		NodeName:  "integration-node",
		Selected:  []string{"vless-reality", "hy2", "ss"},
		Ports: map[string]int{
			"vless-reality": 443,
			"hy2":           8443,
			"ss":            8388,
		},
		WebPort: 8080,
		Token:   "integrationtoken",
		SNI:     "www.bing.com",
	}

	_ = profile
	_ = cfg

	// client, err := sshclient.Dial(profile)
	// if err != nil { t.Fatalf("SSH dial failed: %v", err) }
	// defer client.Close()
	//
	// emit := func(kind string, pct int, msg string) {
	//     t.Logf("[%s %d%%] %s", kind, pct, msg)
	// }
	//
	// result, err := Deploy(client, emit, profile, cfg)
	// if err != nil { t.Fatalf("Deploy returned error: %v", err) }
	// if result["ok"] != true { t.Errorf("Deploy not ok: %v", result) }
}

// ---------------------------------------------------------------------------
// TestDeploy_RetryOnSingboxFailure
// ---------------------------------------------------------------------------
//
// This test would:
//  1. Spin up a Docker container with NO outbound internet access (or block
//     the sing-box download URLs via iptables/hosts).
//  2. Call Deploy() — the first attempt should fail with exit code 11
//     (sing-box install failure).
//  3. Verify that Deploy() detects exit code 11 and triggers the fallback
//     path: uploading a local sing-box binary via SFTP.
//  4. Verify that the second deployment attempt succeeds with the uploaded
//     binary.
//  5. Verify the final result is ok=true.

func TestDeploy_RetryOnSingboxFailure(t *testing.T) {
	t.Skip("requires Docker environment with network isolation")

	// Skeleton: demonstrates the intended test structure.
	profile := config.SSHProfile{
		ID:   "integration-retry",
		Name: "docker-vps-isolated",
		Host: "127.0.0.1",
		User: "root",
		Port: 2223,
	}
	cfg := config.DeployConfig{
		ProfileID: "integration-retry",
		NodeName:  "retry-node",
		Selected:  []string{"ss"},
		Ports:     map[string]int{"ss": 8388},
		WebPort:   8080,
		Token:     "retrytoken",
		SNI:       "www.bing.com",
	}

	_ = profile
	_ = cfg

	// client, err := sshclient.Dial(profile)
	// if err != nil { t.Fatalf("SSH dial failed: %v", err) }
	// defer client.Close()
	//
	// var events []string
	// emit := func(kind string, pct int, msg string) {
	//     events = append(events, fmt.Sprintf("%s:%d", kind, pct))
	//     t.Logf("[%s %d%%] %s", kind, pct, msg)
	// }
	//
	// result, err := Deploy(client, emit, profile, cfg)
	// if err != nil { t.Fatalf("Deploy returned error: %v", err) }
	// if result["ok"] != true { t.Errorf("expected retry to succeed: %v", result) }
	//
	// // Verify the upload fallback path was exercised.
	// foundUpload := false
	// for _, ev := range events {
	//     if strings.Contains(ev, "upload") || strings.Contains(ev, "34") {
	//         foundUpload = true
	//         break
	//     }
	// }
	// if !foundUpload {
	//     t.Log("warning: upload fallback event not detected; verify manually")
	// }
}
