// Run with: go run ./scripts/regression
// Integration checks use synthetic sysfs and never access host hardware.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"fnos-powerguard/internal/powerguard"
	"io"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	root, err := os.MkdirTemp("", "tad-regression-")
	check(err)
	defer os.RemoveAll(root)
	write := func(path, value string) {
		path = filepath.Join(root, path)
		check(os.MkdirAll(filepath.Dir(path), 0700))
		check(os.WriteFile(path, []byte(value), 0600))
	}
	write("proc/cpuinfo", "model name : Intel(R) Processor N100\n")
	write("sys/class/hwmon/hwmon0/name", "coretemp")
	write("sys/class/hwmon/hwmon0/temp1_label", "Core 0")
	write("sys/class/hwmon/hwmon0/temp1_input", "40000")
	write("sys/class/hwmon/hwmon1/name", "it8613")
	for _, n := range []string{"1", "2"} {
		write("sys/class/hwmon/hwmon1/fan"+n+"_input", "1200")
		write("sys/class/hwmon/hwmon1/pwm"+n, "80")
		write("sys/class/hwmon/hwmon1/pwm"+n+"_enable", "2")
	}
	m := &powerguard.Manager{Root: root, ConfigPath: filepath.Join(root, "config.json"), StatePath: filepath.Join(root, "state.json")}
	_, err = m.LoadOrCreateConfig()
	check(err)
	cfg := powerguard.DefaultFanConfig()
	cfg.Enabled = true
	cfg.CPUFanIDs = []string{"it8613:hwmon1:fan1", "it8613:hwmon1:fan2"}
	check(m.SaveFanConfig(cfg))
	cfg.CPUFanIDs = cfg.CPUFanIDs[1:]
	check(m.SaveFanConfig(cfg))
	mode, err := os.ReadFile(filepath.Join(root, "sys/class/hwmon/hwmon1/pwm1_enable"))
	check(err)
	require(strings.TrimSpace(string(mode)) == "2", "removed fan was not restored")
	check(os.Remove(filepath.Join(root, "sys/class/hwmon/hwmon0/temp1_input")))
	require(m.ApplyFanCurrent() != nil, "missing CPU sensor accepted")
	pwm, err := os.ReadFile(filepath.Join(root, "sys/class/hwmon/hwmon1/pwm2"))
	check(err)
	require(strings.TrimSpace(string(pwm)) == "255", "missing CPU sensor did not force full speed")
	script := &powerguard.GPIOScript{ID: "check", Name: "check", Body: "printf forbidden"}
	event := powerguard.GPIOEvent{Action: "script:check", Script: script}
	_, err = m.ExecuteGPIOActionContext(context.Background(), event)
	require(err != nil, "root script enabled by default")
	m.AllowRootScripts = true
	script.Body = "sleep 30 &\nwait\n"
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = m.ExecuteGPIOActionContext(ctx, event)
	require(err != nil && time.Since(start) < 3*time.Second, "script children held output pipes after cancellation")
	// Confirm saved configuration remains valid JSON after the transition.
	data, err := os.ReadFile(m.ConfigPath)
	check(err)
	require(json.Valid(data), "invalid config")
	// Exercise the actual HTTP server over its Unix socket.
	gpio := powerguard.DefaultGPIOConfig()
	gpio.Scripts = []powerguard.GPIOScript{{ID: "private", Name: "private", Body: "secret-script-body"}}
	check(m.SaveGPIOConfig(gpio))
	current, err := user.Current()
	check(err)
	group, err := user.LookupGroupId(current.Gid)
	check(err)
	socket := filepath.Join(root, "readonly.sock")
	server := &powerguard.Server{Manager: m, Socket: socket, WebRoot: root, ReadOnly: true, SocketGroup: group.Name}
	errorsCh := make(chan error, 1)
	go func() { errorsCh <- server.ListenAndServe() }()
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}
	var response *http.Response
	for i := 0; i < 100; i++ {
		response, err = client.Get("http://unix/api/status")
		if err == nil {
			break
		}
		select {
		case err := <-errorsCh:
			check(err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	check(err)
	data, err = io.ReadAll(response.Body)
	response.Body.Close()
	check(err)
	require(response.StatusCode == 200 && !strings.Contains(string(data), "secret-script-body"), "read-only status leaked scripts")
	for _, path := range []string{"/api/config", "/api/config/gpio", "/api/apply", "/api/restore"} {
		request, err := http.NewRequest("POST", "http://unix"+path, strings.NewReader("{}"))
		check(err)
		request.Header.Set("X-Trim-Isadmin", "true")
		response, err = client.Do(request)
		check(err)
		response.Body.Close()
		require(response.StatusCode == http.StatusForbidden, "read-only socket accepted forged admin write")
	}

	fmt.Println("PASS: removed-channel restore, sensor fail-safe, script opt-in, process-group cancellation, read-only HTTP isolation and redaction")
}
func check(err error) {
	if err != nil {
		panic(err)
	}
}
func require(ok bool, message string) {
	if !ok {
		panic(message)
	}
}
