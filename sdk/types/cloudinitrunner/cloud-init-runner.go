package cloudinitrunner

import (
	"github.com/mudler/yip/pkg/schema"
)

type CloudInitRunner interface {
	Run(string, ...string) error
	Analyze(string, ...string)
	SetModifier(schema.Modifier)
}
