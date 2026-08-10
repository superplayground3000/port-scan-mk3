package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type scenario struct {
	name        string
	total       int
	runErr      string
	dispatchErr string
	saveErr     string
	writeFails  bool
	userCancels bool
}

type runtimeState struct {
	phase                string
	contextCancelled     bool
	taskChannelClosed    bool
	resultChannelClosed  bool
	executorErrorDrained bool
	total                int
	enqueued             int
	fullRows             int
	openRows             int
	written              int
	nextIndex            int
	rewoundTo            int
	resumeSaved          bool
	summaryEmitted       bool
	outputsClosed        bool
	runErr               string
	dispatchErr          string
	saveErr              string
}

func main() {
	reader := bufio.NewReader(os.Stdin)

	for {
		printMenu()
		fmt.Print("Select a scenario: ")
		choice, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("read selection: %v\n", err)
			return
		}

		selected, ok := scenarios()[strings.TrimSpace(choice)]
		if !ok {
			if strings.TrimSpace(choice) == "q" {
				return
			}
			fmt.Println("Unknown selection.")
			continue
		}

		fmt.Printf("\nScenario: %s\n", selected.name)
		run(selected)
		fmt.Println()
	}
}

func printMenu() {
	fmt.Println("Deep scan runtime lifecycle")
	fmt.Println("  1  Clean completion")
	fmt.Println("  2  User cancellation")
	fmt.Println("  3  Output write failure")
	fmt.Println("  4  Runtime and dispatcher errors")
	fmt.Println("  5  Snapshot save failure")
	fmt.Println("  q  Quit")
}

func scenarios() map[string]scenario {
	return map[string]scenario{
		"1": {name: "clean completion", total: 1},
		"2": {
			name:        "user cancellation saves incomplete work",
			total:       2,
			dispatchErr: "context canceled",
			userCancels: true,
		},
		"3": {
			name:       "output failure rewinds an unwritten task",
			total:      1,
			runErr:     "write scan output: disk full",
			writeFails: true,
		},
		"4": {
			name:        "runtime error wins over dispatcher error",
			total:       1,
			runErr:      "pressure source failed",
			dispatchErr: "context canceled",
		},
		"5": {
			name:       "snapshot save error wins over runtime error",
			total:      1,
			runErr:     "write scan output: disk full",
			saveErr:    "save resume snapshot: permission denied",
			writeFails: true,
		},
	}
}

func run(selected scenario) {
	state := runtimeState{total: selected.total, rewoundTo: -1}

	state.phase = "prepared"
	emit("load inputs and snapshot, then build the run plan", state)

	state.phase = "outputs_open"
	emit("open or validate both output files", state)

	state.phase = "running"
	emit("create child context, then start monitors and workers", state)

	state.enqueued = 1
	state.nextIndex = 1
	emit("dispatcher enqueues one task and advances its cursor", state)

	state.taskChannelClosed = true
	emit("dispatcher closes the task channel", state)

	if selected.userCancels {
		state.fullRows = 1
		state.openRows = 1
		state.written = 1
		emit("both output writes succeed and commit the result", state)

		state.contextCancelled = true
		state.dispatchErr = selected.dispatchErr
		emit("caller cancellation stops dispatch", state)
	} else if selected.writeFails {
		state.fullRows = 1
		state.runErr = selected.runErr
		state.contextCancelled = true
		emit("the second output write fails and the runtime cancels work", state)
	} else {
		state.fullRows = 1
		state.openRows = 1
		state.written = 1
		emit("both output writes succeed and commit the result", state)

		if selected.runErr != "" {
			state.runErr = selected.runErr
			state.contextCancelled = true
			emit("first runtime error cancels work", state)
		}
	}

	if selected.dispatchErr != "" && state.dispatchErr == "" {
		state.dispatchErr = selected.dispatchErr
		emit("record the dispatcher error separately", state)
	}

	state.phase = "draining"
	state.resultChannelClosed = true
	state.executorErrorDrained = true
	emit("drain results and executor errors after workers stop", state)

	state.phase = "reconciling"
	emit("close all rate-limit buckets", state)

	if selected.writeFails {
		state.rewoundTo = 0
		state.nextIndex = 0
		emit("rewind the earliest unwritten task", state)
	}

	if shouldSave(state) {
		state.phase = "saving"
		if selected.saveErr != "" {
			state.saveErr = selected.saveErr
			emit("snapshot save fails", state)
		} else {
			state.resumeSaved = true
			emit("save resumable state with resolved output paths", state)
		}
	}

	state.phase = "summarizing"
	state.summaryEmitted = true
	emit("select the final error and emit one completion summary", state)

	state.phase = "complete"
	state.outputsClosed = true
	emit("close outputs without replacing the selected error", state)

	fmt.Printf("RETURN: %s\n", finalError(state))
}

func shouldSave(state runtimeState) bool {
	return state.nextIndex < state.total ||
		state.runErr != "" || state.dispatchErr != ""
}

func finalError(state runtimeState) string {
	switch {
	case state.saveErr != "":
		return state.saveErr
	case state.runErr != "":
		return state.runErr
	case state.dispatchErr != "":
		return state.dispatchErr
	default:
		return "nil"
	}
}

func emit(action string, state runtimeState) {
	fmt.Printf("\nACTION: %s\n", action)
	fmt.Printf(
		"STATE: phase=%s cancelled=%t channels={task_closed:%t result_closed:%t executor_error_drained:%t}\n",
		state.phase,
		state.contextCancelled,
		state.taskChannelClosed,
		state.resultChannelClosed,
		state.executorErrorDrained,
	)
	fmt.Printf(
		"       progress={total:%d enqueued:%d committed:%d next_index:%d rewound_to:%d}\n",
		state.total,
		state.enqueued,
		state.written,
		state.nextIndex,
		state.rewoundTo,
	)
	fmt.Printf(
		"       output={full_rows:%d open_rows:%d} durable={resume_saved:%t outputs_closed:%t}\n",
		state.fullRows,
		state.openRows,
		state.resumeSaved,
		state.outputsClosed,
	)
	fmt.Printf(
		"       errors={run:%q dispatch:%q save:%q} summary_emitted=%t\n",
		state.runErr,
		state.dispatchErr,
		state.saveErr,
		state.summaryEmitted,
	)
}
