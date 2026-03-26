package translator

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"

	"github.com/wsmbsbbz/tts-ijc/server/domain"
)

var progressRe = regexp.MustCompile(`\[(\d+)/(\d+)\]`)

// PythonTranslator implements domain.Translator by shelling out to python main.py.
type PythonTranslator struct {
	pythonBin string
	pythonDir string
}

// NewPythonTranslator creates a PythonTranslator.
// pythonBin is the Python executable (e.g. "python3").
// pythonDir is the directory containing main.py.
func NewPythonTranslator(pythonBin, pythonDir string) *PythonTranslator {
	return &PythonTranslator{
		pythonBin: pythonBin,
		pythonDir: pythonDir,
	}
}

func (t *PythonTranslator) Execute(ctx context.Context, input domain.TranslateInput, onProgress func(domain.TranslateProgress)) error {
	args := []string{
		"main.py",
		input.AudioPath,
		input.VTTPath,
		input.OutputPath,
		"--tts", input.Config.TTSProvider,
		"--tts-volume", fmt.Sprintf("%.3f", input.Config.TTSVolume),
		"--concurrency", strconv.Itoa(input.Config.Concurrency),
	}
	if input.Config.NoSpeedup {
		args = append(args, "--no-speedup")
	}

	cmd := exec.CommandContext(ctx, t.pythonBin, args...)
	cmd.Dir = t.pythonDir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	cmd.Stderr = cmd.Stdout // merge stderr into stdout for unified progress reading

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start python: %w", err)
	}

	var lastLines []string
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		lastLines = append(lastLines, line)
		if len(lastLines) > 20 {
			lastLines = lastLines[len(lastLines)-20:]
		}
		if onProgress != nil {
			if p, ok := parseProgress(line); ok {
				onProgress(p)
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		tail := ""
		if len(lastLines) > 0 {
			tail = ": " + lastLines[len(lastLines)-1]
		}
		return fmt.Errorf("python exited with error: %w%s", err, tail)
	}

	return nil
}

func parseProgress(line string) (domain.TranslateProgress, bool) {
	matches := progressRe.FindStringSubmatch(line)
	if len(matches) < 3 {
		return domain.TranslateProgress{}, false
	}

	current, err1 := strconv.Atoi(matches[1])
	total, err2 := strconv.Atoi(matches[2])
	if err1 != nil || err2 != nil {
		return domain.TranslateProgress{}, false
	}

	return domain.TranslateProgress{
		Current: current,
		Total:   total,
		Message: line,
	}, true
}
