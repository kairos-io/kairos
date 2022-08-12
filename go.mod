module github.com/c3os-io/c3os

go 1.17

require (
	github.com/avast/retry-go v3.0.0+incompatible
	github.com/bramvdbogaerde/go-scp v1.2.0
	github.com/c3os-io/c3os/sdk v0.0.0-00010101000000-000000000000
	github.com/denisbrodbeck/machineid v1.0.1
	github.com/erikgeiser/promptkit v0.6.0
	github.com/google/go-github/v40 v40.0.0
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510
	github.com/jaypipes/ghw v0.9.0
	github.com/joho/godotenv v1.4.0
	github.com/mudler/go-nodepair v0.0.0-20220507212557-7d47aa3cc1f1
	github.com/mudler/go-pluggable v0.0.0-20220716112424-189d463e3ff3
	github.com/mudler/go-processmanager v0.0.0-20211226182900-899fbb0b97f6
	github.com/mudler/yip v0.0.0-20220725150231-976737b2353c
	github.com/nxadm/tail v1.4.8
	github.com/onsi/ginkgo/v2 v2.1.3
	github.com/onsi/gomega v1.19.0
	github.com/pterm/pterm v0.12.41
	github.com/qeesung/image2ascii v1.0.1
	github.com/urfave/cli v1.22.9
	golang.org/x/crypto v0.0.0-20220622213112-05595931fe9d
	golang.org/x/oauth2 v0.0.0-20220223155221-ee480838109b
	gopkg.in/yaml.v2 v2.4.0
)

require (
	github.com/StackExchange/wmi v1.2.1 // indirect
	github.com/atomicgo/cursor v0.0.1 // indirect
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aybabtme/rgbterm v0.0.0-20170906152045-cc83f3b3ce59 // indirect
	github.com/charmbracelet/bubbles v0.10.3 // indirect
	github.com/charmbracelet/bubbletea v0.20.0 // indirect
	github.com/charmbracelet/lipgloss v0.5.0 // indirect
	github.com/chuckpreslar/emission v0.0.0-20170206194824-a7ddd980baf9 // indirect
	github.com/containerd/console v1.0.3 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.2 // indirect
	github.com/disintegration/imaging v1.6.2 // indirect
	github.com/eliukblau/pixterm v1.3.1 // indirect
	github.com/fsnotify/fsnotify v1.5.4 // indirect
	github.com/gen2brain/shm v0.0.0-20200228170931-49f9650110c5 // indirect
	github.com/ghodss/yaml v1.0.0 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/gookit/color v1.5.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/itchyny/gojq v0.12.8 // indirect
	github.com/itchyny/timefmt-go v0.1.3 // indirect
	github.com/jaypipes/pcidb v1.0.0 // indirect
	github.com/jezek/xgb v0.0.0-20210312150743-0e0f116e1240 // indirect
	github.com/kbinani/screenshot v0.0.0-20210720154843-7d3a670d8329 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/lxn/win v0.0.0-20210218163916-a377121e959e // indirect
	github.com/makiuchi-d/gozxing v0.1.1 // indirect
	github.com/mattn/go-isatty v0.0.14 // indirect
	github.com/mattn/go-runewidth v0.0.13 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/muesli/ansi v0.0.0-20211018074035-2e021307bc4b // indirect
	github.com/muesli/reflow v0.3.0 // indirect
	github.com/muesli/termenv v0.11.1-0.20220212125758-44cd13922739 // indirect
	github.com/nfnt/resize v0.0.0-20180221191011-83c6a9932646 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/rivo/uniseg v0.2.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e // indirect
	github.com/twpayne/go-vfs v1.7.2 // indirect
	github.com/wayneashleyberry/terminal-dimensions v1.1.0 // indirect
	github.com/xo/terminfo v0.0.0-20210125001918-ca9a967f8778 // indirect
	golang.org/x/image v0.0.0-20191206065243-da761ea9ff43 // indirect
	golang.org/x/net v0.0.0-20220630215102-69896b714898 // indirect
	golang.org/x/sys v0.0.0-20220704084225-05e143d24a9e // indirect
	golang.org/x/term v0.0.0-20220526004731-065cf7ba2467 // indirect
	golang.org/x/text v0.3.7 // indirect
	golang.org/x/xerrors v0.0.0-20220517211312-f3a8303e98df // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/protobuf v1.28.0 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
	gopkg.in/yaml.v1 v1.0.0-20140924161607-9f9df34309c0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	howett.net/plist v1.0.0 // indirect
)

replace github.com/elastic/gosigar => github.com/mudler/gosigar v0.14.3-0.20220502202347-34be910bdaaf

replace github.com/c3os-io/c3os/sdk => ./sdk
