#!/usr/bin/env python3
"""Run one local external Task consumer.

The configured command receives the protobuf payload on stdin. Metadata is
provided through FFDB_TASK_ID, FFDB_TASK_TYPE and FFDB_LEASE_EPOCH. Exit 0
completes the task; any other exit status fails it. Payload and command output
are never printed by this wrapper.
"""

import argparse
import os
import random
import subprocess
import sys
import time
from pathlib import Path

import grpc

PB_PATH = Path(__file__).resolve().parents[1] / "twitter" / "pb"
sys.path.insert(0, str(PB_PATH))
import api_pb2  # noqa: E402
import api_pb2_grpc  # noqa: E402


def parse_args():
    parser = argparse.ArgumentParser()
    parser.add_argument("--target", default="127.0.0.1:8901")
    parser.add_argument("--worker-id", required=True)
    parser.add_argument("--type", action="append", dest="types", required=True)
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    if not args.target.startswith(("127.0.0.1:", "localhost:", "[::1]:")):
        parser.error("target must be loopback")
    if not args.command:
        parser.error("a handler command is required after --")
    if args.command[0] == "--":
        args.command = args.command[1:]
    return args


def execute(stub, args, task):
    env = os.environ.copy()
    env.update(
        FFDB_TASK_ID=task.id,
        FFDB_TASK_TYPE=task.type,
        FFDB_LEASE_EPOCH=str(task.lease_epoch),
    )
    process = subprocess.Popen(args.command, stdin=subprocess.PIPE, env=env)
    process.stdin.write(task.payload)
    process.stdin.close()
    lease_until = task.lease_until_ms
    while process.poll() is None:
        remaining = lease_until / 1000 - time.time()
        if remaining <= 0:
            process.terminate()
            raise RuntimeError("task lease expired")
        time.sleep(max(0.05, min(remaining / 2, 5.0)))
        if process.poll() is not None:
            break
        try:
            renewed = stub.RenewTaskLease(
                api_pb2.RenewTaskLeaseRequest(
                    worker_id=args.worker_id,
                    task_id=task.id,
                    lease_epoch=task.lease_epoch,
                )
            )
        except Exception:
            process.terminate()
            process.wait()
            raise
        lease_until = renewed.lease_until_ms
    if process.returncode == 0:
        stub.CompleteTask(
            api_pb2.CompleteTaskRequest(
                worker_id=args.worker_id,
                task_id=task.id,
                lease_epoch=task.lease_epoch,
            )
        )
    else:
        stub.FailTask(
            api_pb2.FailTaskRequest(
                worker_id=args.worker_id,
                task_id=task.id,
                lease_epoch=task.lease_epoch,
                error=f"handler exited with status {process.returncode}",
            )
        )


def main():
    args = parse_args()
    delay = 1.0
    with grpc.insecure_channel(args.target) as channel:
        stub = api_pb2_grpc.ApiStub(channel)
        while True:
            response = stub.ClaimTasks(
                api_pb2.ClaimTasksRequest(
                    worker_id=args.worker_id, types=args.types, max_tasks=1
                )
            )
            if not response.tasks:
                time.sleep(delay * random.uniform(0.8, 1.2))
                delay = min(delay * 2, 30.0)
                continue
            delay = 1.0
            execute(stub, args, response.tasks[0])


if __name__ == "__main__":
    main()
