package rtapi

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func fileRow(path string, size, chunks, completed uint64, priority int) string {
	return "<array><data><value>" + stringValue(path) + "</value><value>" + intValue(size) +
		"</value><value>" + intValue(chunks) + "</value><value>" + intValue(completed) +
		"</value><value>" + intValue(uint64(priority)) + "</value></data></array>"
}

func TestFilesListsEveryFile(t *testing.T) {
	client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		var params []string
		for _, param := range call.Params {
			params = append(params, *param.Value.String)
		}
		want := []string{testHash, "", "f.path=", "f.size_bytes=", "f.size_chunks=", "f.completed_chunks=", "f.priority="}
		if call.MethodName != "f.multicall" || !reflect.DeepEqual(params, want) {
			t.Errorf("request = %s %v", call.MethodName, params)
		}
		return arrayResponse(fileRow("Show/e01.mkv", 1000, 4, 4, 1), fileRow("Show/sample.mkv", 10, 1, 0, 0))
	})
	files, err := client.Files(testHash)
	if err != nil {
		t.Fatal(err)
	}
	want := []File{
		{Index: 0, Path: "Show/e01.mkv", Size: 1000, Chunks: 4, CompletedChunks: 4, Priority: FileNormal},
		{Index: 1, Path: "Show/sample.mkv", Size: 10, Chunks: 1, CompletedChunks: 0, Priority: FileSkip},
	}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("files = %+v", files)
	}
	if !files[0].Complete() || files[1].Complete() {
		t.Fatal("Complete is wrong")
	}
}

func TestSetFilePrioritiesTargetsFilesAndApplies(t *testing.T) {
	var calls []nestedCall
	client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		calls = nestedCalls(call)
		results := make([]string, len(calls))
		for i := range results {
			results[i] = result(intValue(0))
		}
		return arrayResponse(results...)
	})
	if err := client.SetFilePriorities(testHash, map[int]FilePriority{3: FileHigh, 0: FileSkip}); err != nil {
		t.Fatal(err)
	}
	want := []nestedCall{
		{"f.priority.set", []string{testHash + ":f0", "0"}},
		{"f.priority.set", []string{testHash + ":f3", "2"}},
		{"d.update_priorities", []string{testHash}},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %+v", calls)
	}

	for _, bad := range []map[int]FilePriority{{-1: FileNormal}, {0: FilePriority(3)}} {
		if err := client.SetFilePriorities(testHash, bad); err == nil {
			t.Errorf("SetFilePriorities(%v) succeeded", bad)
		}
	}
	if err := client.SetFilePriorities("", map[int]FilePriority{0: FileSkip}); err == nil {
		t.Error("empty hash accepted")
	}
}

func TestGlobalLimits(t *testing.T) {
	var calls []nestedCall
	client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		calls = nestedCalls(call)
		if strings.HasSuffix(calls[0].method, ".set") {
			return arrayResponse(result(intValue(0)), result(intValue(0)))
		}
		return arrayResponse(result(intValue(5<<20)), result(intValue(0)))
	})
	down, up, err := client.GlobalLimits()
	if err != nil || down != 5<<20 || up != 0 {
		t.Fatalf("GlobalLimits = %d, %d, %v", down, up, err)
	}
	if err := client.SetGlobalLimits(2<<20, 512<<10); err != nil {
		t.Fatal(err)
	}
	want := []nestedCall{
		{"throttle.global_down.max_rate.set", []string{"", "2097152"}},
		{"throttle.global_up.max_rate.set", []string{"", "524288"}},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("set calls = %+v", calls)
	}
}

func TestFreeDiskSpace(t *testing.T) {
	client := testClient(t, func(_ string, call xmlrpcMethodCall) string {
		if call.MethodName != "d.free_diskspace" || len(call.Params) != 1 {
			t.Errorf("request = %s with %d params", call.MethodName, len(call.Params))
		}
		if *call.Params[0].Value.String == strings.Repeat("F", 40) {
			return topLevelFault(-501, "Could not find info-hash.")
		}
		return response(intValue(42 << 30))
	})
	free, err := client.FreeDiskSpace(testHash)
	if err != nil || free != 42<<30 {
		t.Fatalf("FreeDiskSpace = %d, %v", free, err)
	}
	var rpcFault *XMLRPCFault
	if _, err := client.FreeDiskSpace(strings.Repeat("F", 40)); !errors.As(err, &rpcFault) {
		t.Fatalf("expected a fault, got %v", err)
	}
}
