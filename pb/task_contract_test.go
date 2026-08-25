package pb

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestTaskQueueRPCsAreRegistered(t *testing.T) {
	service := File_api_proto.Services().ByName("Api")
	if service == nil {
		t.Fatal("Api service descriptor is missing")
	}
	for _, name := range []protoreflect.Name{
		"EnqueueTask", "ClaimTasks", "CompleteTask", "FailTask", "RenewTaskLease",
	} {
		if service.Methods().ByName(name) == nil {
			t.Errorf("Api.%s descriptor is missing", name)
		}
	}
}

func TestTaskQueueEnumsKeepZeroUnspecified(t *testing.T) {
	if TaskState_TASK_STATE_UNSPECIFIED != 0 ||
		TaskCompletionStatus_TASK_COMPLETION_STATUS_UNSPECIFIED != 0 ||
		FailTaskOutcome_FAIL_TASK_OUTCOME_UNSPECIFIED != 0 {
		t.Fatal("Task enum zero values must remain unspecified")
	}
}
