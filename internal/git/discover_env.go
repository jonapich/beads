package git

import "os"

// discoveryEnvOverrides are the environment variables that change how Git
// finds or validates a repository. When any is present, discovery is left to
// git itself. The list is deliberately broader than strictly necessary: a
// fallback costs one subprocess, a wrong answer costs a wrong workspace.
var discoveryEnvOverrides = []string{
	"GIT_DIR",
	"GIT_WORK_TREE",
	"GIT_COMMON_DIR",
	"GIT_CEILING_DIRECTORIES",
	"GIT_DISCOVERY_ACROSS_FILESYSTEM",
	"GIT_OBJECT_DIRECTORY",
	"GIT_CONFIG_PARAMETERS",
	"GIT_CONFIG_COUNT",
	"GIT_TEST_ASSUME_DIFFERENT_OWNER",
}

func discoveryEnvOverridden() bool {
	for _, name := range discoveryEnvOverrides {
		if _, set := os.LookupEnv(name); set {
			return true
		}
	}
	return false
}
