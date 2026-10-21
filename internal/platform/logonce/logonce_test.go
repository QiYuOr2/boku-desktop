package logonce

import (
	"sync"
	"testing"
)

func TestShouldWritesTheSameMessageOnce(t *testing.T) {
	Reset()
	if !Should("shortcut", "用不了 Ctrl+Alt+T：被占用") {
		t.Fatal("the first record was dropped")
	}
	if Should("shortcut", "用不了 Ctrl+Alt+T：被占用") {
		t.Error("the repeated record was written again")
	}
}

func TestShouldWritesAgainWhenTheMessageChanges(t *testing.T) {
	Reset()
	Should("clipboard", "磁盘已满")
	if !Should("clipboard", "权限不足") {
		t.Error("a different failure under the same key was dropped")
	}
}

func TestKeysAreIndependent(t *testing.T) {
	Reset()
	Should("first", "same message")
	if !Should("second", "same message") {
		t.Error("a message already seen under another key was dropped")
	}
}

func TestForgetWritesTheNextMessage(t *testing.T) {
	Reset()
	Should("shortcut", "失败")
	Forget("shortcut")
	if !Should("shortcut", "失败") {
		t.Error("the message after Forget was dropped")
	}
}

func TestForgetLeavesOtherKeysAlone(t *testing.T) {
	Reset()
	Should("first", "失败")
	Should("second", "失败")
	Forget("first")
	if Should("second", "失败") {
		t.Error("Forget on another key made this one write again")
	}
}

func TestShouldIsSafeFromSeveralGoroutines(t *testing.T) {
	Reset()
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Should("shared", "同一个错误")
		}()
	}
	wg.Wait()
	// The map must still answer, and a repeat is still a repeat.
	if Should("shared", "同一个错误") {
		t.Error("a repeat after concurrent writes was reported as new")
	}
}
