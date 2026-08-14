package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/yinhm/friendfeed/server"
	"github.com/yinhm/friendfeed/store"
	taskqueue "github.com/yinhm/friendfeed/task"
)

func runListTasksCommand(db *store.Store) {
	limit := timelineMaxLimit
	if limit == 0 {
		limit = 100
	}
	records, err := taskqueue.List(db, taskState, limit)
	if err != nil {
		log.Fatal(err)
	}
	for _, record := range records {
		fmt.Printf("id=%s type=%s state=%s attempts=%d run_at=%d lease_until=%d last_error=%q\n",
			record.Task.Id, record.Task.Type, record.Task.State, record.Task.Attempts,
			record.Task.RunAtMs, record.Task.LeaseUntilMs, record.Task.LastError)
	}
	log.Printf("listed %d %s tasks", len(records), taskState)
}

func runInspectTaskCommand(db *store.Store) {
	if inspectID == "" {
		log.Fatal("-id is required for inspect_task")
	}
	record, err := taskqueue.Inspect(db, inspectID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("task=%v\n", record.Task)
	if record.Completion != nil {
		fmt.Printf("completion=%v\n", record.Completion)
	}
}

func runReplayDeadTaskCommand(db *store.Store) {
	if inspectID == "" {
		log.Fatal("-id is required for replay_dead_task")
	}
	registry, err := server.NewTaskRegistry(nil)
	if err != nil {
		log.Fatal(err)
	}
	queue, err := taskqueue.NewQueue(db, registry, taskqueue.Options{})
	if err != nil {
		log.Fatal(err)
	}
	result, err := queue.ReplayDead(context.Background(), inspectID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("replayed %s as %s\n", inspectID, result.Task.Id)
}

func runPurgeTaskDoneCommand(db *store.Store) {
	if beforeTime == "" {
		log.Fatal("-before RFC3339 is required for purge_task_done")
	}
	cutoff, err := time.Parse(time.RFC3339, beforeTime)
	if err != nil {
		log.Fatalf("parse -before: %v", err)
	}
	if !dryRun {
		if err := confirmDestructive("purge_task_done", toPath, os.Stdin, os.Stderr); err != nil {
			log.Fatal(err)
		}
	}
	count, err := taskqueue.PurgeDone(db, cutoff, dryRun)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("TaskDone rows before %s: %d (dry-run=%t)", cutoff.UTC().Format(time.RFC3339), count, dryRun)
}
