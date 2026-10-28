package outlet

import "testing"

func TestReadMemory(t *testing.T) {
	memory, err := readMemory()
	if err != nil || memory.total == 0 || memory.free > memory.total {
		t.Fatalf("readMemory = %+v, %v", memory, err)
	}
}
