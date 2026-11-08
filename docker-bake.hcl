# Builds and tests bd with nothing but Docker on the host.
#
# Build bd.exe into dist/windows-amd64 (from PowerShell):
#   $env:BD_COMMIT = git rev-parse HEAD
#   $env:BD_BRANCH = git branch --show-current
#   docker buildx bake
#
# Check for references to the original project's infrastructure:
#   docker buildx bake guard
#
# Run the test suite, or one package with its flags:
#   docker buildx bake test
#   $env:BD_TEST_ARGS = "./internal/config/..."; docker buildx bake test

variable "BD_COMMIT" {
  default = ""
}

variable "BD_BRANCH" {
  default = ""
}

variable "BD_TEST_ARGS" {
  default = ""
}

group "default" {
  targets = ["guard", "windows-amd64"]
}

target "guard" {
  target = "guard"
  output = ["type=cacheonly"]
}

target "windows-amd64" {
  target = "windows-amd64"
  args = {
    BD_COMMIT = BD_COMMIT
    BD_BRANCH = BD_BRANCH
  }
  output = ["type=local,dest=dist/windows-amd64"]
}

target "test" {
  target = "test"
  args = {
    BD_TEST_ARGS = BD_TEST_ARGS
  }
  output = ["type=cacheonly"]
}
