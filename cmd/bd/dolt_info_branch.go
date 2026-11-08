package main

import "os"

// doltRemoteInfoBranchEnv is Dolt's switch for the __dolt_remote_info__ branch.
// An empty value turns the branch off.
const doltRemoteInfoBranchEnv = "DOLT_REMOTE_INFO_BRANCH"

func init() {
	disableDoltRemoteInfoBranch()
}

// disableDoltRemoteInfoBranch stops Dolt from force-pushing its informational
// branch after every remote write. The branch only tells people browsing the
// repository that it holds Dolt data, and each update costs an extra connection
// to the remote. Setting the variable yourself takes precedence.
func disableDoltRemoteInfoBranch() {
	if _, set := os.LookupEnv(doltRemoteInfoBranchEnv); set {
		return
	}
	_ = os.Setenv(doltRemoteInfoBranchEnv, "")
}
