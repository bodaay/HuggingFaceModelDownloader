package hfclient

import (
	"fmt"
	"sort"
	"strings"
)

type node struct {
	name     string
	isFile   bool
	children map[string]*node
	file     *File // Only set for files
}

func newNode(name string, isFile bool) *node {
	return &node{
		name:     name,
		isFile:   isFile,
		children: make(map[string]*node),
	}
}

func buildTree(files []*File) *node {
	root := newNode("", false)

	for _, file := range files {
		parts := strings.Split(file.Path, "/")
		current := root

		// Create/traverse the path
		for i, part := range parts {
			isFile := i == len(parts)-1
			if next, exists := current.children[part]; exists {
				current = next
			} else {
				next := newNode(part, isFile)
				if isFile {
					next.file = file
				}
				current.children[part] = next
				current = next
			}
		}
	}

	return root
}

func PrintFileTree(files []*File) {
	root := buildTree(files)
	printNode(root, "", true)
}

func printNode(n *node, prefix string, isLast bool) {
	if n.name != "" {
		marker := "├── "
		if isLast {
			marker = "└── "
		}

		// Print the node
		size := ""
		if n.isFile && n.file != nil {
			size = formatSize(n.file.Size)
			if n.file.IsLFS {
				size += " (LFS)"
			}
		}

		fmt.Printf("%s%s%s %s\n", prefix, marker, n.name, size)
	}

	// Get and sort children
	var children []*node
	for _, child := range n.children {
		children = append(children, child)
	}
	sort.Slice(children, func(i, j int) bool {
		// Directories come before files
		if children[i].isFile != children[j].isFile {
			return !children[i].isFile
		}
		return children[i].name < children[j].name
	})

	// Print children
	for i, child := range children {
		newPrefix := prefix
		if n.name != "" {
			if isLast {
				newPrefix += "    "
			} else {
				newPrefix += "│   "
			}
		}
		printNode(child, newPrefix, i == len(children)-1)
	}
}

func formatSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGTPE"[exp])
}
