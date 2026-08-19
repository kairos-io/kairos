package matchers

import (
	"fmt"
	"os"

	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/types"
	"github.com/twpayne/go-vfs/v5"
)

// BeAnExistingFileFs returns a matcher that checks if a file exists in the given vfs.
func BeAnExistingFileFs(fs vfs.FS) types.GomegaMatcher {
	return &beAnExistingFileFsMatcher{
		fs: fs,
	}
}

type beAnExistingFileFsMatcher struct {
	fs vfs.FS
}

func (matcher *beAnExistingFileFsMatcher) Match(actual interface{}) (success bool, err error) {
	actualFilename, ok := actual.(string)
	if !ok {
		return false, fmt.Errorf("BeAnExistingFileFs matcher expects a file path")
	}
	// Here is the magic, check existence against a vfs
	if _, err = matcher.fs.Stat(actualFilename); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func (matcher *beAnExistingFileFsMatcher) FailureMessage(actual interface{}) (message string) {
	return format.Message(actual, "to exist")
}

func (matcher *beAnExistingFileFsMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return format.Message(actual, "not to exist")
}
