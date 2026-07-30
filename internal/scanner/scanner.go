package scanner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Verdict string

const (
	VerdictClean    Verdict = "clean"
	VerdictInfected Verdict = "infected"
)

type Metadata struct {
	Path     string
	MIMEType string
	Size     int64
}

type Result struct {
	Verdict Verdict
	Detail  string
}

// Scanner receives the completed object as a stream. Implementations must not
// retain file contents after Scan returns.
type Scanner interface {
	Scan(ctx context.Context, metadata Metadata, content io.Reader) (Result, error)
}

type Noop struct{}

func (Noop) Scan(context.Context, Metadata, io.Reader) (Result, error) {
	return Result{Verdict: VerdictClean}, nil
}

type CommandOptions struct {
	Command           []string
	Timeout           time.Duration
	CleanExitCodes    []int
	InfectedExitCodes []int
}

type Command struct {
	command  []string
	timeout  time.Duration
	clean    map[int]struct{}
	infected map[int]struct{}
}

func NewCommand(options CommandOptions) (*Command, error) {
	if len(options.Command) == 0 || strings.TrimSpace(options.Command[0]) == "" {
		return nil, errors.New("scanner command is required")
	}
	if options.Timeout <= 0 {
		return nil, errors.New("scanner timeout must be positive")
	}
	result := &Command{
		command: append([]string(nil), options.Command...), timeout: options.Timeout,
		clean: make(map[int]struct{}), infected: make(map[int]struct{}),
	}
	for _, code := range options.CleanExitCodes {
		result.clean[code] = struct{}{}
	}
	for _, code := range options.InfectedExitCodes {
		if _, duplicate := result.clean[code]; duplicate {
			return nil, fmt.Errorf("scanner exit code %d cannot be both clean and infected", code)
		}
		result.infected[code] = struct{}{}
	}
	if len(result.clean) == 0 || len(result.infected) == 0 {
		return nil, errors.New("scanner clean and infected exit codes are required")
	}
	return result, nil
}

func (s *Command) Scan(ctx context.Context, metadata Metadata, content io.Reader) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	arguments := make([]string, 0, len(s.command)-1)
	for _, argument := range s.command[1:] {
		argument = strings.ReplaceAll(argument, "{path}", metadata.Path)
		argument = strings.ReplaceAll(argument, "{mime}", metadata.MIMEType)
		argument = strings.ReplaceAll(argument, "{size}", strconv.FormatInt(metadata.Size, 10))
		arguments = append(arguments, argument)
	}
	command := exec.CommandContext(ctx, s.command[0], arguments...)
	command.Stdin = content
	output := &cappedBuffer{maximum: 32 * 1024}
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return Result{}, fmt.Errorf("scanner timed out after %s", s.timeout)
	}
	exitCode := 0
	if err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			return Result{}, fmt.Errorf("run scanner: %w", err)
		}
		exitCode = exitError.ExitCode()
	}
	detail := strings.TrimSpace(output.String())
	if _, ok := s.clean[exitCode]; ok {
		return Result{Verdict: VerdictClean, Detail: detail}, nil
	}
	if _, ok := s.infected[exitCode]; ok {
		return Result{Verdict: VerdictInfected, Detail: detail}, nil
	}
	return Result{}, fmt.Errorf("scanner returned unclassified exit code %d: %s", exitCode, detail)
}

type cappedBuffer struct {
	buffer  bytes.Buffer
	maximum int
}

func (w *cappedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	remaining := w.maximum - w.buffer.Len()
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		_, _ = w.buffer.Write(value)
	}
	return original, nil
}

func (w *cappedBuffer) String() string { return w.buffer.String() }
