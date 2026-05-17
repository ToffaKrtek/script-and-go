package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	// "github.com/creack/pty"
	ssh_config "github.com/kevinburke/ssh_config"
	"github.com/manifoldco/promptui"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

type Script struct {
	Name  string `yaml:"name"`
	Steps []Step `yaml:"steps"`
}

type Step struct {
	SSH        string      `yaml:"ssh,omitempty"`
	Cmd        string      `yaml:"cmd,omitempty"`
	Input      interface{} `yaml:"input,omitempty"`
	Display    string      `yaml:"display,omitempty"`
	InputToCmd bool        `yaml:"input_to_cmd,omitempty"`
	ShowOutput bool        `yaml:"show_output,omitempty"`
}

func loadScripts() ([]Script, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".script-and-go")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("директория %s не существует", dir)
		}
		return nil, err
	}
	var scripts []Script
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Ошибка чтения файла %s: %v\n", path, err)
			continue
		}
		var s Script
		if err := yaml.Unmarshal(data, &s); err != nil {
			fmt.Fprintf(os.Stderr, "Ошибка парсинга %s: %v\n", path, err)
			continue
		}
		scripts = append(scripts, s)
	}
	return scripts, nil
}

func promptForInput(display string, isSecret bool) (string, error) {
	if display == "" {
		display = "Ввод: "
	}
	fmt.Printf("%s", display)
	if isSecret {
		bytePwd, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			return "", err
		}
		return string(bytePwd), nil
	}
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(input), nil
}

func createSSHClient(host string) (*ssh.Client, error) {
	user := ssh_config.Get(host, "User")
	if user == "" {
		user = os.Getenv("USER")
	}
	hostname := ssh_config.Get(host, "HostName")
	if hostname == "" {
		hostname = host
	}
	port := ssh_config.Get(host, "Port")
	if port == "" {
		port = "22"
	}
	authMethods := []ssh.AuthMethod{}
	if agentConn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		agentClient := agent.NewClient(agentConn)
		authMethods = append(authMethods, ssh.PublicKeysCallback(agentClient.Signers))
		defer agentConn.Close()
	}
	identityFiles := ssh_config.GetAll(host, "IdentityFile")
	for _, file := range identityFiles {
		file = os.ExpandEnv(file)
		key, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err == nil {
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	addr := fmt.Sprintf("%s:%s", hostname, port)
	return ssh.Dial("tcp", addr, config)
}

func runCommand(cmdStr string, stdinData string, showOutput bool, sshClient *ssh.Client) error {
	if sshClient != nil {
		session, err := sshClient.NewSession()
		if err != nil {
			return err
		}
		defer session.Close()
		modes := ssh.TerminalModes{
			ssh.ECHO:          0,
			ssh.TTY_OP_ISPEED: 14400,
			ssh.TTY_OP_OSPEED: 14400,
		}
		_ = session.RequestPty("xterm", 80, 40, modes)

		stdin, _ := session.StdinPipe()
		stdout, _ := session.StdoutPipe()
		stderr, _ := session.StderrPipe()
		if err := session.Start(cmdStr); err != nil {
			return err
		}
		if stdinData != "" {
			go func() {
				defer stdin.Close()
				io.WriteString(stdin, stdinData+"\n")
			}()
		}
		if showOutput {
			go io.Copy(os.Stdout, stdout)
			go io.Copy(os.Stderr, stderr)
			return session.Wait()
		} else {
			var outBuf, errBuf bytes.Buffer
			go func() { io.Copy(&outBuf, stdout) }()
			go func() { io.Copy(&errBuf, stderr) }()
			if err := session.Wait(); err != nil {
				return err
			}
			if outBuf.Len() > 0 {
				fmt.Print(outBuf.String())
			}
			if errBuf.Len() > 0 {
				fmt.Fprintln(os.Stderr, errBuf.String())
			}
			return nil
		}
		return session.Wait()
	}
	cmd := exec.Command("sh", "-c", cmdStr)

	if stdinData != "" {
		cmd.Stdin = strings.NewReader(stdinData + "\n")
	}

	if showOutput {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	} else {
		var outBuf, errBuf bytes.Buffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
		err := cmd.Run()
		if outBuf.Len() > 0 {
			fmt.Print(outBuf.String())
		}
		if errBuf.Len() > 0 {
			fmt.Fprint(os.Stderr, errBuf.String())
		}
		return err
	}
}

func runScript(script Script) error {
	var currentSSHClient *ssh.Client
	var lastInput string

	for i, step := range script.Steps {
		fmt.Printf("\n[%d/%d ", i+1, len(script.Steps))
		if step.SSH != "" {
			fmt.Printf("Подключение к %s...\n", step.SSH)
			if currentSSHClient != nil {
				currentSSHClient.Close()
				currentSSHClient = nil
			}
			client, err := createSSHClient(step.SSH)
			if err != nil {
				return fmt.Errorf("ошибка подключения к %s: %w", step.SSH, err)
			}
			currentSSHClient = client
			continue
		}
		if step.Cmd != "" {
			cmd := step.Cmd
			needInput := false
			isSecret := false
			switch v := step.Input.(type) {
			case bool:
				needInput = v
			case string:
				if v == "*" {
					needInput = true
					isSecret = true
				}
			}
			var inputValue string
			if needInput {
				var err error
				inputValue, err = promptForInput(step.Display, isSecret)
				if err != nil {
					return err
				}
				lastInput = inputValue
			} else if step.Display != "" {
				fmt.Println(step.Display)
			}
			if step.InputToCmd && lastInput != "" {
				cmd = strings.ReplaceAll(cmd, "${input}", lastInput)
			}

			fmt.Printf("Выполнение команды %s...\n", cmd)
			err := runCommand(cmd, inputValue, step.ShowOutput, currentSSHClient)
			if err != nil {
				return fmt.Errorf("ошибка выполнения команды %s: %w", cmd, err)
			}
			continue
		}
		fmt.Println("Пропуск некорректного шага")
	}
	if currentSSHClient != nil {
		currentSSHClient.Close()
	}
	return nil
}

func main() {
	scripts, err := loadScripts()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка загрузки скриптов: %v\n", err)
		os.Exit(1)
	}
	if len(scripts) == 0 {
		fmt.Println("Нет сценариев в ~/.script-and-go")
		os.Exit(0)
	}
	names := make([]string, len(scripts))
	for i, s := range scripts {
		names[i] = s.Name
	}
	prompt := promptui.Select{
		Label: "Выберите сценарий",
		Items: names,
	}
	idx, _, err := prompt.Run()
	if err != nil {
		fmt.Printf("Выход: %v\n", err)
		os.Exit(0)
	}
	selected := scripts[idx]
	fmt.Printf("\n=== Запуск сценария: %s ===\n", selected.Name)
	if err := runScript(selected); err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка выполнения сценария: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("\n=== Сценарий завершен ===\n")
}
