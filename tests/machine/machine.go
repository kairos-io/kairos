package machine

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/bramvdbogaerde/go-scp"
	"github.com/c3os-io/c3os/internal/utils"

	//. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/crypto/ssh"
)

var ID string
var TempDir string

func Delete() {
	utils.SH(fmt.Sprintf(`VBoxManage controlvm "%s" poweroff`, ID))
	utils.SH(fmt.Sprintf(`VBoxManage unregistervm --delete "%s"`, ID))
	utils.SH(fmt.Sprintf(`VBoxManage closemedium disk "%s"`, filepath.Join(TempDir, "disk.vdi")))
	os.RemoveAll(TempDir)
	utils.SH(fmt.Sprintf("rm -rf ~/VirtualBox\\ VMs/%s", ID))

}

func Create(sshPort string) {
	out, err := utils.SH(fmt.Sprintf("VBoxManage createmedium disk --filename %s --size %d", filepath.Join(TempDir, "disk.vdi"), 30000))
	fmt.Println(out)
	Expect(err).ToNot(HaveOccurred())

	out, err = utils.SH(fmt.Sprintf("VBoxManage createvm --name %s --register", ID))
	fmt.Println(out)
	Expect(err).ToNot(HaveOccurred())

	out, err = utils.SH(fmt.Sprintf("VBoxManage modifyvm %s --memory 10040 --cpus 3", ID))
	fmt.Println(out)
	Expect(err).ToNot(HaveOccurred())

	out, err = utils.SH(fmt.Sprintf(`VBoxManage modifyvm %s --nic1 nat --boot1 disk --boot2 dvd --natpf1 "guestssh,tcp,,%s,,22"`, ID, sshPort))
	fmt.Println(out)
	Expect(err).ToNot(HaveOccurred())

	out, err = utils.SH(fmt.Sprintf(`VBoxManage storagectl "%s" --name "sata controller" --add sata --portcount 2 --hostiocache off`, ID))
	fmt.Println(out)
	Expect(err).ToNot(HaveOccurred())

	out, err = utils.SH(fmt.Sprintf(`VBoxManage storageattach "%s" --storagectl "sata controller" --port 0 --device 0 --type hdd --medium %s`, ID, filepath.Join(TempDir, "disk.vdi")))
	fmt.Println(out)
	Expect(err).ToNot(HaveOccurred())

	out, err = utils.SH(fmt.Sprintf(`VBoxManage storageattach "%s" --storagectl "sata controller" --port 1 --device 0 --type dvddrive --medium %s`, ID, os.Getenv("ISO")))
	fmt.Println(out)
	Expect(err).ToNot(HaveOccurred())

	out, err = utils.SH(fmt.Sprintf(`VBoxManage startvm "%s" --type headless`, ID))
	fmt.Println(out)
	Expect(err).ToNot(HaveOccurred())
}
func HasDir(s string) {
	out, err := SSHCommand("if [ -d " + s + " ]; then echo ok; else echo wrong; fi")
	Expect(err).ToNot(HaveOccurred())
	Expect(out).Should(Equal("ok\n"))
}

func EventuallyConnects(t ...int) {
	dur := 360
	if len(t) > 0 {
		dur = t[0]
	}
	Eventually(func() string {
		out, _ := SSHCommand("echo ping")
		return out
	}, time.Duration(time.Duration(dur)*time.Second), time.Duration(5*time.Second)).Should(Equal("ping\n"))
}

func SSHCommand(cmd string) (string, error) {
	client, session, err := connectToHost()
	if err != nil {
		return "", err
	}
	defer client.Close()
	out, err := session.CombinedOutput(cmd)
	if err != nil {
		return string(out), err
	}

	return string(out), err
}

func DetachCD() error {
	_, err := utils.SH(fmt.Sprintf(`VBoxManage storageattach "%s" --storagectl "sata controller" --port 1 --device 0 --medium none`, ID))
	return err
}

func Restart() error {
	_, err := utils.SH(fmt.Sprintf(`VBoxManage controlvm "%s" reset`, ID))
	return err
}

func SendFile(src, dst, permission string) error {
	sshConfig := &ssh.ClientConfig{
		User:    user(),
		Auth:    []ssh.AuthMethod{ssh.Password(pass())},
		Timeout: 30 * time.Second, // max time to establish connection
	}
	sshConfig.HostKeyCallback = ssh.InsecureIgnoreHostKey()

	scpClient := scp.NewClientWithTimeout(host(), sshConfig, 10*time.Second)
	defer scpClient.Close()

	if err := scpClient.Connect(); err != nil {
		return err
	}

	f, err := os.Open(src)
	if err != nil {
		return err
	}

	defer scpClient.Close()
	defer f.Close()

	if err := scpClient.CopyFile(context.Background(), f, dst, permission); err != nil {
		return err
	}
	return nil
}

func host() string {
	host := fmt.Sprintf("%s:%s", os.Getenv("SSH_HOST"), os.Getenv("SSH_PORT"))
	if host == "" || host == ":" {
		host = "127.0.0.1:2222"
	}
	return host
}

func user() string {
	user := os.Getenv("SSH_USER")
	if user == "" {
		user = "c3os"
	}
	return user
}

func pass() string {
	pass := os.Getenv("SSH_PASS")
	if pass == "" {
		pass = "c3os"
	}

	return pass
}

func connectToHost() (*ssh.Client, *ssh.Session, error) {
	sshConfig := &ssh.ClientConfig{
		User:    user(),
		Auth:    []ssh.AuthMethod{ssh.Password(pass())},
		Timeout: 30 * time.Second, // max time to establish connection
	}

	sshConfig.HostKeyCallback = ssh.InsecureIgnoreHostKey()

	client, err := SSHDialTimeout("tcp", host(), sshConfig, 30*time.Second)
	if err != nil {
		return nil, nil, err
	}

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, nil, err
	}

	return client, session, nil
}

func Sudo(c string) (string, error) {
	return SSHCommand(fmt.Sprintf(`sudo /bin/sh -c "%s"`, c))
}

// GatherAllLogs will try to gather as much info from the system as possible, including services, dmesg and os related info
func GatherAllLogs(services []string, logFiles []string) {
	// services
	for _, ser := range services {
		out, err := Sudo(fmt.Sprintf("journalctl -u %s -o short-iso >> /run/%s.log", ser, ser))
		if err != nil {
			fmt.Printf("Error getting journal for service %s: %s\n", ser, err.Error())
			fmt.Printf("Output from command: %s\n", out)
		}
		GatherLog(fmt.Sprintf("/run/%s.log", ser))
	}

	// log files
	for _, file := range logFiles {
		GatherLog(file)
	}

	// dmesg
	out, err := Sudo("dmesg > /run/dmesg")
	if err != nil {
		fmt.Printf("Error getting dmesg : %s\n", err.Error())
		fmt.Printf("Output from command: %s\n", out)
	}
	GatherLog("/run/dmesg")

	// grab full journal
	out, err = Sudo("journalctl -o short-iso > /run/journal.log")
	if err != nil {
		fmt.Printf("Error getting full journalctl info : %s\n", err.Error())
		fmt.Printf("Output from command: %s\n", out)
	}
	GatherLog("/run/journal.log")

	// uname
	out, err = Sudo("uname -a > /run/uname.log")
	if err != nil {
		fmt.Printf("Error getting uname info : %s\n", err.Error())
		fmt.Printf("Output from command: %s\n", out)
	}
	GatherLog("/run/uname.log")

	// disk info
	out, err = Sudo("lsblk -a >> /run/disks.log")
	if err != nil {
		fmt.Printf("Error getting disk info : %s\n", err.Error())
		fmt.Printf("Output from command: %s\n", out)
	}
	out, err = Sudo("blkid >> /run/disks.log")
	if err != nil {
		fmt.Printf("Error getting disk info : %s\n", err.Error())
		fmt.Printf("Output from command: %s\n", out)
	}
	GatherLog("/run/disks.log")

	// Grab users
	GatherLog("/etc/passwd")
	// Grab system info
	GatherLog("/etc/os-release")
}

// GatherLog will try to scp the given log from the machine to a local file
func GatherLog(logPath string) {
	Sudo("chmod 777 " + logPath)
	fmt.Printf("Trying to get file: %s\n", logPath)
	sshConfig := &ssh.ClientConfig{
		User:    user(),
		Auth:    []ssh.AuthMethod{ssh.Password(pass())},
		Timeout: 30 * time.Second, // max time to establish connection
	}
	sshConfig.HostKeyCallback = ssh.InsecureIgnoreHostKey()

	scpClient := scp.NewClientWithTimeout(host(), sshConfig, 10*time.Second)
	defer scpClient.Close()

	err := scpClient.Connect()
	if err != nil {
		fmt.Println("Couldn't establish a connection to the remote server ", err)
		return
	}

	baseName := filepath.Base(logPath)
	_ = os.Mkdir("logs", 0755)

	f, _ := os.Create(fmt.Sprintf("logs/%s", baseName))
	// Close the file after it has been copied
	// Close client connection after the file has been copied
	defer scpClient.Close()
	defer f.Close()

	ctx, can := context.WithTimeout(context.Background(), 2*time.Minute)
	defer can()
	err = scpClient.CopyFromRemote(ctx, f, logPath)
	if err != nil {
		fmt.Printf("Error while copying file: %s\n", err.Error())
		return
	}
	// Change perms so its world readable
	_ = os.Chmod(fmt.Sprintf("logs/%s", baseName), 0666)
	fmt.Printf("File %s copied!\n", baseName)

}

type Conn struct {
	net.Conn
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

func (c *Conn) Read(b []byte) (int, error) {
	err := c.Conn.SetReadDeadline(time.Now().Add(c.ReadTimeout))
	if err != nil {
		return 0, err
	}
	return c.Conn.Read(b)
}

func (c *Conn) Write(b []byte) (int, error) {
	err := c.Conn.SetWriteDeadline(time.Now().Add(c.WriteTimeout))
	if err != nil {
		return 0, err
	}
	return c.Conn.Write(b)
}

func SSHDialTimeout(network, addr string, config *ssh.ClientConfig, timeout time.Duration) (*ssh.Client, error) {
	conn, err := net.DialTimeout(network, addr, timeout)
	if err != nil {
		return nil, err
	}

	timeoutConn := &Conn{conn, timeout, timeout}
	c, chans, reqs, err := ssh.NewClientConn(timeoutConn, addr, config)
	if err != nil {
		return nil, err
	}
	client := ssh.NewClient(c, chans, reqs)

	// this sends keepalive packets every 2 seconds
	// there's no useful response from these, so we can just abort if there's an error
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for range t.C {
			_, _, err := client.Conn.SendRequest("keepalive@golang.org", true, nil)
			if err != nil {
				return
			}
		}
	}()
	return client, nil
}
