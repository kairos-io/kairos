package machine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bramvdbogaerde/go-scp"
	"github.com/c3os-io/c3os/cli/utils"

	//. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/crypto/ssh"
)

var ID string
var TempDir string

func Delete() {
	utils.SH(fmt.Sprintf(`VBoxManage controlvm "%s" poweroff`, ID))
	utils.SH(fmt.Sprintf(`VBoxManage unregistervm "%s"`, ID))
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

	client, err := ssh.Dial("tcp", host(), sshConfig)
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
