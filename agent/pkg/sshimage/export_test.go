package sshimage

// SetRemoteLayout points the client at another command line file and state
// directory on the remote node, and returns a restorer.
func SetRemoteLayout(cmdline, stateDir string) func() {
	pc, ps := remoteCmdline, remoteStateDir
	remoteCmdline, remoteStateDir = cmdline, stateDir
	return func() { remoteCmdline, remoteStateDir = pc, ps }
}

// BootedImage exposes the command line parsing.
func BootedImage(cmdline string) (string, error) { return bootedImage(cmdline) }
