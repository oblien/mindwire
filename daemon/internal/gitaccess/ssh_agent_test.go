package gitaccess

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// This process stands in for ssh at the exec boundary and uses the real agent
// wire protocol to prove that the selected key can sign without a private file.
func sshAgentProbe(args []string) int {
	var socket, identity string
	for i, arg := range args {
		if arg == "-i" && i+1 < len(args) {
			identity = args[i+1]
		}
		if strings.HasPrefix(arg, "IdentityAgent=") {
			socket = strings.TrimPrefix(arg, "IdentityAgent=")
		}
	}
	if socket == "" || identity == "" {
		return 2
	}
	data, err := os.ReadFile(identity)
	if err != nil || bytes.Contains(data, []byte("PRIVATE KEY")) {
		return 3
	}
	public, _, _, _, err := ssh.ParseAuthorizedKey(data)
	if err != nil {
		return 4
	}
	entries, err := os.ReadDir(filepath.Dir(identity))
	if err != nil {
		return 5
	}
	for _, entry := range entries {
		if entry.Name() != "identity.pub" && entry.Name() != "agent.sock" {
			return 6
		}
	}
	connection, err := net.Dial("unix", socket)
	if err != nil {
		return 7
	}
	defer connection.Close()
	client := sshagent.NewClient(connection)
	keys, err := client.List()
	if err != nil || len(keys) != 1 || !bytes.Equal(keys[0].Marshal(), public.Marshal()) {
		return 8
	}
	message := []byte("mindwire SSH authentication fixture")
	signature, err := client.Sign(keys[0], message)
	if err != nil || public.Verify(message, signature) != nil {
		return 9
	}
	fmt.Fprintln(os.Stdout, "agent-verified")
	return 0
}

func TestSSHHelperSignsWithoutWritingPrivateKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix process fixture")
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	auth := Auth{Kind: "ssh", PrivateKey: string(pem.EncodeToMemory(block))}
	s := service(t)
	c := Connection{ID: "ssh-fixture", Login: "fixture", Mode: "sshKey", Lifetime: "run"}
	lease, err := s.Prepare(context.Background(), "git@github.com:octocat/app.git", &c, &auth)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	t.Setenv(leaseEnvironmentKey, lease.Environment[leaseEnvironmentKey])
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(executable, filepath.Join(dir, "ssh")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MINDWIRE_SSH_AGENT_PROBE", "1")
	var output, diagnostics bytes.Buffer
	err = SSHHelper([]string{"lease", s.endpoint, "octocat/app", "git@github.com", "git-upload-pack 'octocat/app.git'"}, strings.NewReader(""), &output, &diagnostics)
	if err != nil || strings.TrimSpace(output.String()) != "agent-verified" {
		t.Fatalf("SSH key agent: %v %s", err, diagnostics.String())
	}
	if _, err := os.Stat(s.path); !os.IsNotExist(err) {
		t.Fatal("run-only SSH key was saved")
	}
}

func TestSSHHelperAuthenticatesWithNativeSSHUsingMemoryAgent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix SSH integration")
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if !bytes.Equal(key.Marshal(), signer.PublicKey().Marshal()) {
			return nil, fmt.Errorf("unexpected identity")
		}
		return nil, nil
	}}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	served := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			served <- err
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(10 * time.Second))
		server, channels, requests, err := ssh.NewServerConn(connection, config)
		if err != nil {
			served <- err
			return
		}
		defer server.Close()
		go ssh.DiscardRequests(requests)
		incoming, ok := <-channels
		if !ok {
			served <- fmt.Errorf("missing SSH session")
			return
		}
		channel, commands, err := incoming.Accept()
		if err != nil {
			served <- err
			return
		}
		defer channel.Close()
		for request := range commands {
			if request.Type != "exec" {
				_ = request.Reply(false, nil)
				continue
			}
			var command struct{ Command string }
			if err := ssh.Unmarshal(request.Payload, &command); err != nil || command.Command != "git-upload-pack 'octocat/app.git'" {
				served <- fmt.Errorf("unexpected Git command")
				return
			}
			_ = request.Reply(true, nil)
			_, _ = channel.Write([]byte("native-ssh-verified\n"))
			_, err := channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
			served <- err
			return
		}
		served <- fmt.Errorf("SSH session did not execute Git")
	}()
	block, err := ssh.MarshalPrivateKey(private, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	s := service(t)
	c := Connection{ID: "ssh-native", Login: "fixture", Mode: "sshKey", Lifetime: "run"}
	lease, err := s.Prepare(context.Background(), "git@github.com:octocat/app.git", &c, &Auth{Kind: "ssh", PrivateKey: string(pem.EncodeToMemory(block))})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	t.Setenv(leaseEnvironmentKey, lease.Environment[leaseEnvironmentKey])
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	args := []string{"lease", s.endpoint, "octocat/app", "-F", os.DevNull, "-o", "HostName=127.0.0.1", "-p", port,
		"-o", "UserKnownHostsFile=" + filepath.Join(t.TempDir(), "known_hosts"), "-o", "GlobalKnownHostsFile=" + os.DevNull,
		"git@github.com", "git-upload-pack 'octocat/app.git'"}
	var output, diagnostics bytes.Buffer
	if err := SSHHelper(args, strings.NewReader(""), &output, &diagnostics); err != nil {
		t.Fatalf("native SSH rejected the memory agent: %v %s", err, diagnostics.String())
	}
	if err := <-served; err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) != "native-ssh-verified" {
		t.Fatalf("missing SSH output: %s", output.String())
	}
}
