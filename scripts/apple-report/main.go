// Command apple-report emits shareable, credential-free XCTest case outcomes.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

type testNode struct {
	NodeType       string
	NodeIdentifier string
	Result         string
	Duration       string
	Children       []testNode
}
type outcome struct {
	Test     string `json:"test"`
	Result   string `json:"result"`
	Duration string `json:"duration,omitempty"`
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
			cases = append(cases, outcome{node.NodeIdentifier, node.Result, node.Duration})
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
