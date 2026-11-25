package agent

// P2PServiceUnitForTest and GenerateUpgradeConfForCLIArgsForTest expose
// unexported helpers to the external test package.
func P2PServiceUnitForTest(bin string) string { return p2pServiceUnit(bin) }

func GenerateUpgradeConfForCLIArgsForTest(source string) (string, error) {
	return generateUpgradeConfForCLIArgs(source, "", false)
}
