package run

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/ToffaKrtek/script-and-go/types"
	ssh_config "github.com/kevinburke/ssh_config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/term"
)

func RunScript(script types.Script) error {
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

func promptForInput(display string, isSecret bool) (string, error) {
	if display == "" {
		display = "Ввод: "
	}
	fmt.Printf("%s", display)
	if isSecret {
		bytePwd, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println("")
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
