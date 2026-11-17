package ops

import _ "embed"

// maasCurtinHook is the Kairos MAAS curtin-hook, placed at /curtin/curtin-hooks
// in the dedicated curtin-landing partition (COS_CURTIN) of a disk.maas image.
// curtin selects that partition as its target and runs the hook on deploy; the
// hook then mounts COS_OEM by label and writes the datasource/network handoff
// there. Source: pkg/ops/assets/maas-curtin-hooks.
//
//go:embed assets/maas-curtin-hooks
var maasCurtinHook []byte
