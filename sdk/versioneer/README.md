_ -eer suffix :  (in nouns) a person concerned with a particular thing` (https://www.oxfordlearnersdictionaries.com)

Versioneer is a library and a wrapping cli that is concerned with everything related to artifact names and versions in Kairos.

There is a standalone CLI in the bin/versioneer directory of thie repository. It's also embedded in kairos-agent
as a command "kairos-agent versioneer". This allows to use it wherever we have kairos-agent available (e.g. within a Kairos OS)
or inside a CI pipeline (by running the standalone cli directly).
