// +build ignore

/*
 * Minio Cloud Storage, (C) 2015 Minio, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func genLDFlags(version string) string {
	var ldflagsStr string
	ldflagsStr = "\"-X main.minioVersion=" + version + " "
	ldflagsStr = ldflagsStr + "-X main.minioReleaseTag=" + releaseTag(version) + " "
	ldflagsStr = ldflagsStr + "-X main.minioCommitID=" + commitID() + " "
	ldflagsStr = ldflagsStr + "-X main.minioShortCommitID=" + commitID()[:12] + "\""
	return ldflagsStr
}

// genReleaseTag prints release tag to the console for easy git tagging.
func releaseTag(version string) string {
	relPrefix := "UNOFFICIAL"
	if prefix := os.Getenv("MINIO_RELEASE"); prefix != "" {
		relPrefix = prefix
	}

	relTag := strings.Replace(version, " ", "-", -1)
	relTag = strings.Replace(relTag, ":", "-", -1)
	relTag = strings.Replace(relTag, ",", "", -1)
	return relPrefix + "." + relTag
}

// commitID returns the abbreviated commit-id hash of the last commit.
func commitID() string {
	// git log --format="%h" -n1
	var (
		commit []byte
		e      error
	)
	cmdName := "git"
	cmdArgs := []string{"log", "--format=%H", "-n1"}
	if commit, e = exec.Command(cmdName, cmdArgs...).Output(); e != nil {
		fmt.Fprintln(os.Stderr, "Error generating git commit-id: ", e)
		os.Exit(1)
	}

	return strings.TrimSpace(string(commit))
}

func main() {
	fmt.Println(genLDFlags(time.Now().UTC().Format(time.RFC3339)))
}
