package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const version = "1.0.0"

var reset = "\033[0m"
var bold = "\033[1m"
var red = "\033[31m"
var green = "\033[32m"
var yellow = "\033[33m"
var cyan = "\033[36m"
var dim = "\033[2m"

func main() {
	if len(os.Args) < 2 {
		printHelp()
		return
	}

	switch os.Args[1] {
	case "df", "disk":
		runDiskUsage()
	case "clean":
		runClean(os.Args[2:])
	case "images":
		runImages()
	case "ps":
		runContainers()
	case "volumes":
		runVolumes()
	case "nuke":
		runNuke(os.Args[2:])
	case "stop-all":
		runStopAll()
	case "version", "-v", "--version":
		fmt.Printf("tidydock v%s\n", version)
	case "help", "-h", "--help":
		printHelp()
	default:
		fmt.Printf("%s Unknown command: %s%s\n", red, os.Args[1], reset)
		printHelp()
	}
}

// ── Disk Usage ──────────────────────────────────────────────────────────────

func runDiskUsage() {
	printHeader("Docker Disk Usage")
	out := dockerRun("system", "df")
	fmt.Println(out)
}

// ── List Images ─────────────────────────────────────────────────────────────

func runImages() {
	printHeader("Docker Images")
	out := dockerRun("images", "--format", "table {{.Repository}}\t{{.Tag}}\t{{.ID}}\t{{.CreatedSince}}\t{{.Size}}")
	fmt.Println(out)
}

// ── List Containers ─────────────────────────────────────────────────────────

func runContainers() {
	printHeader("All Containers")
	out := dockerRun("ps", "-a", "--format", "table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}")
	fmt.Println(out)
}

// ── List Volumes ─────────────────────────────────────────────────────────────

func runVolumes() {
	printHeader("Docker Volumes")
	out := dockerRun("volume", "ls")
	fmt.Println(out)
}

// ── Stop All ─────────────────────────────────────────────────────────────────

func runStopAll() {
	printHeader("Stopping All Containers")
	ids := strings.TrimSpace(dockerRun("ps", "-q"))
	if ids == "" {
		fmt.Printf("%s No running containers.%s\n", green, reset)
		return
	}
	for _, id := range strings.Split(ids, "\n") {
		out := dockerRun("stop", id)
		fmt.Printf("  %s Stopped: %s%s\n", green, strings.TrimSpace(out), reset)
	}
}

// ── Clean ────────────────────────────────────────────────────────────────────

func runClean(args []string) {
	keep := parseKeepFlag(args)

	printHeader("Docker Clean")

	if keep != "" {
		fmt.Printf("%s Keeping image: %s%s\n\n", cyan, keep, reset)
	}

	// Show disk usage first
	fmt.Printf("%s%s Current disk usage:%s\n", bold, cyan, reset)
	fmt.Println(dockerRun("system", "df"))

	steps := []struct {
		label string
		fn    func() string
	}{
		{"Pruning stopped containers", func() string {
			return dockerRun("container", "prune", "-f")
		}},
		{"Pruning build cache", func() string {
			return dockerRun("builder", "prune", "-a", "-f")
		}},
		{"Pruning unused volumes", func() string {
			return dockerRun("volume", "prune", "-f")
		}},
	}

	for _, step := range steps {
		fmt.Printf("%s --> %s...%s\n", yellow, step.label, reset)
		out := step.fn()
		printReclaimed(out)
	}

	// Remove images (respecting --keep)
	fmt.Printf("%s --> Removing unused images...%s\n", yellow, reset)
	removeImages(keep)

	fmt.Printf("\n%s%s Done! Run 'tidydock df' to see new disk usage.%s\n", bold, green, reset)
}

// ── Nuke ─────────────────────────────────────────────────────────────────────

func runNuke(args []string) {
	keep := parseKeepFlag(args)

	printHeader("!! NUKE MODE !!")

	fmt.Printf("%s This will remove ALL Docker data", red)
	if keep != "" {
		fmt.Printf(" except '%s'", keep)
	}
	fmt.Printf(".%s\n", reset)
	fmt.Printf("%s This cannot be undone. Type 'yes' to confirm: %s", red, reset)

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input != "yes" {
		fmt.Printf("%s Aborted.%s\n", yellow, reset)
		return
	}

	fmt.Println()

	// Stop all containers first
	ids := strings.TrimSpace(dockerRun("ps", "-q"))
	if ids != "" {
		fmt.Printf("%s --> Stopping all containers...%s\n", yellow, reset)
		for _, id := range strings.Split(ids, "\n") {
			dockerRun("stop", id)
		}
	}

	steps := []struct {
		label string
		fn    func() string
	}{
		{"Removing all containers", func() string {
			return dockerRun("container", "prune", "-f")
		}},
		{"Clearing build cache", func() string {
			return dockerRun("builder", "prune", "-a", "-f")
		}},
		{"Removing all volumes", func() string {
			return dockerRun("volume", "prune", "-a", "-f")
		}},
	}

	for _, step := range steps {
		fmt.Printf("%s --> %s...%s\n", yellow, step.label, reset)
		out := step.fn()
		printReclaimed(out)
	}

	fmt.Printf("%s --> Removing all images...%s\n", yellow, reset)
	removeImages(keep)

	fmt.Printf("\n%s%s Nuke complete. Fresh start! %s\n", bold, green, reset)
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func removeImages(keep string) {
	// Get all image IDs with their repo:tag
	out := dockerRun("images", "--format", "{{.Repository}}:{{.Tag}} {{.ID}}")
	lines := strings.Split(strings.TrimSpace(out), "\n")

	removed := 0
	skipped := 0

	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		repoTag := parts[0]
		id := parts[1]

		if keep != "" && strings.Contains(repoTag, keep) {
			fmt.Printf("  %s Keeping: %s%s\n", cyan, repoTag, reset)
			skipped++
			continue
		}

		result := dockerRun("rmi", "-f", id)
		if strings.Contains(result, "Error") || strings.Contains(result, "error") {
			fmt.Printf("  %s Could not remove: %s %s%s\n", dim, repoTag, result, reset)
		} else {
			fmt.Printf("  %s Removed: %s%s\n", green, repoTag, reset)
			removed++
		}
	}

	fmt.Printf("  %s Removed %d images, kept %d.%s\n", green, removed, skipped, reset)
}

func parseKeepFlag(args []string) string {
	for i, arg := range args {
		if arg == "--keep" || arg == "-k" {
			if i+1 < len(args) {
				return args[i+1]
			}
		}
		if after, ok :=strings.CutPrefix(arg, "--keep="); ok  {
			return after
		}
	}
	return ""
}

func dockerRun(args ...string) string {
	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(out))
	}
	return strings.TrimSpace(string(out))
}

func printReclaimed(out string) {
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(strings.ToLower(line), "reclaimed") ||
			strings.Contains(strings.ToLower(line), "deleted") ||
			strings.Contains(strings.ToLower(line), "total") {
			fmt.Printf("  %s %s%s\n", green, line, reset)
		}
	}
}

func printHeader(title string) {
	line := strings.Repeat("─", len(title)+4)
	fmt.Printf("\n%s%s┌%s┐%s\n", bold, cyan, line, reset)
	fmt.Printf("%s%s│  %s  │%s\n", bold, cyan, title, reset)
	fmt.Printf("%s%s└%s┘%s\n\n", bold, cyan, line, reset)
}

func printHelp() {
	fmt.Printf(`
%s%s tidydock v%s — Docker Management CLI %s

%sUSAGE:%s
  tidydock <command> [flags]

%sCOMMANDS:%s
  df, disk       Show Docker disk usage
  images         List all images
  ps             List all containers
  volumes        List all volumes
  stop-all       Stop all running containers
  clean          Clean unused data (safe)
  nuke           Remove everything (destructive)
  version        Show version

%sFLAGS:%s
  --keep, -k     Image name to preserve during clean/nuke
                 e.g. tidydock clean --keep egekocabas/remote-docker
                      tidydock nuke --keep postgres

%sEXAMPLES:%s
  tidydock df
  tidydock clean
  tidydock clean --keep egekocabas/remote-docker
  tidydock nuke --keep postgres
  tidydock images
  tidydock stop-all

`, bold+cyan, "", version, reset, bold, reset, bold, reset, bold, reset, bold, reset)
}