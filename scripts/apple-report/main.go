// Command apple-report emits shareable, credential-free XCTest case outcomes.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type testNode struct {
	Name           string
	NodeType       string
	NodeIdentifier string
	Result         string
	Duration       string
	Children       []testNode
	SourceLocation *struct {
		FilePath   string
		LineNumber int
	}
}
type outcome struct {
	Test             string   `json:"test"`
	Result           string   `json:"result"`
	Duration         string   `json:"duration,omitempty"`
	FailureLocations []string `json:"failureLocations,omitempty"`
	FailureKinds     []string `json:"failureKinds,omitempty"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: apple-report BUNDLE.xcresult")
		os.Exit(2)
	}
	if err := report(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func report(bundle string) error {
	cmd := exec.Command("xcrun", "xcresulttool", "get", "test-results", "tests", "--path", bundle)
	cmd.Stderr = os.Stderr
	data, err := cmd.Output()
	if err != nil {
		return err
	}
	var tree struct{ TestNodes []testNode }
	if err = json.Unmarshal(data, &tree); err != nil {
		return err
	}
	cases := []outcome{}
	var visit func(testNode)
	visit = func(node testNode) {
		if node.NodeType == "Test Case" {
			item := outcome{Test: node.NodeIdentifier, Result: node.Result, Duration: node.Duration}
			for _, child := range node.Children {
				if child.NodeType != "Failure Message" {
					continue
				}
				if loc := child.SourceLocation; loc != nil {
					item.FailureLocations = append(item.FailureLocations, fmt.Sprintf("%s:%d", filepath.Base(loc.FilePath), loc.LineNumber))
				}
				// XCTest messages can contain pairing links, typed input or terminal data.
				// Publish only recognized framework diagnostics.
				reason := strings.ToLower(child.Name)
				for _, kind := range []string{
					"failed to get screenshot", "failed to synthesize event",
					"failed to get matching snapshots", "failed to get snapshot",
					"not hittable", "not in the foreground", "failed to type",
					"failed to click", "failed to tap", "failed to install or launch",
					"invalid device state", "timed out", "xctasserttrue failed", "xctassertequal failed",
				} {
					if strings.Contains(reason, kind) {
						item.FailureKinds = append(item.FailureKinds, kind)
					}
				}
			}
			cases = append(cases, item)
		}
		for _, child := range node.Children {
			visit(child)
		}
	}
	for _, node := range tree.TestNodes {
		visit(node)
	}
	if len(cases) == 0 {
		return fmt.Errorf("no XCTest cases in %s", bundle)
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		Cases []outcome `json:"cases"`
	}{cases})
}
