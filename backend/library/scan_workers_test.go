package library

import (
	"context"
	"testing"

	"yellowjacket/backend/system"
)

// How many workers a scan gets is decided by two facts about the
// device, and the second one is new: a spinning disk that can queue
// commands wants several reads in flight, and one that cannot wants
// almost none.  Before this it was a flat 2 for anything rotational,
// which is a pre-NCQ assumption — a modern SATA disk reports a queue
// depth of 32 and was being given a quarter of what it can use.
func TestWorkersForProfile(t *testing.T) {
	t.Parallel()

	const cpus = 16

	ssd := system.DiskProfile{Device: "sda", QueueDepth: 32}
	hddQueued := system.DiskProfile{
		Device: "sdb", Rotational: true, QueueDepth: 32,
	}
	hddSerial := system.DiskProfile{
		Device: "sdc", Rotational: true, QueueDepth: 1,
	}
	// Neither NVMe nor a device-mapper volume publishes queue_depth.
	// An unknown depth must not be read as "cannot queue", or every
	// such device would be scanned as if it were a 2003 drive.
	unknown := system.DiskProfile{Device: "dm-0", Rotational: true}

	tests := []struct {
		name    string
		mode    ScanConcurrency
		profile system.DiskProfile
		want    int
	}{
		{"ssd auto", ScanConcurrencyAuto, ssd, cpus},
		{"queueing hdd auto", ScanConcurrencyAuto, hddQueued, hddWorkerCountQueued},
		{"serial hdd auto", ScanConcurrencyAuto, hddSerial, hddWorkerCountSerial},
		{"unknown depth queues", ScanConcurrencyAuto, unknown, hddWorkerCountQueued},

		// The mode overrules detection about the *disk*, never about
		// its queue: forcing hdd on a queueing drive still uses it.
		{"forced hdd on an ssd", ScanConcurrencyHDD, ssd, hddWorkerCountQueued},
		{"forced ssd on an hdd", ScanConcurrencySSD, hddQueued, cpus},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := workersForProfile(tt.mode, tt.profile, cpus); got != tt.want {
				t.Errorf(
					"workersForProfile(%q, %+v) = %d, want %d",
					tt.mode, tt.profile, got, tt.want,
				)
			}
		})
	}
}

// A machine with fewer cores than the policy asks for gets its cores.
func TestWorkersNeverExceedTheCPUCount(t *testing.T) {
	t.Parallel()

	hdd := system.DiskProfile{Rotational: true, QueueDepth: 32}

	if got := workersForProfile(ScanConcurrencyAuto, hdd, 1); got != 1 {
		t.Errorf("single-core hdd = %d workers, want 1", got)
	}
}

// The prefetch stage must forward every item and nothing else: it is a
// pass-through with a side effect, and a scan that drops a file because
// of a *hint* would be a spectacular way to lose part of a library.
func TestReadaheadForwardsEveryFile(t *testing.T) {
	t.Parallel()

	in := make(chan scanWork, 4)
	for _, p := range []string{"/a", "/b", "/c", "/d"} {
		in <- scanWork{absolutePath: p}
	}

	close(in)

	var got []string
	for w := range readaheadWork(
		context.Background(),
		in,
		system.DiskProfile{Rotational: true, QueueDepth: 32},
	) {
		got = append(got, w.absolutePath)
	}

	want := []string{"/a", "/b", "/c", "/d"}
	if len(got) != len(want) {
		t.Fatalf("forwarded %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("item %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// On an SSD the stage is not inserted at all — the channel comes back
// unchanged, so a scan there pays nothing for a feature it cannot use.
func TestReadaheadIsSkippedOnSolidState(t *testing.T) {
	t.Parallel()

	in := make(chan scanWork)
	out := readaheadWork(
		context.Background(), in, system.DiskProfile{QueueDepth: 32},
	)

	if out != (<-chan scanWork)(in) {
		t.Error("an ssd must get the original channel, unwrapped")
	}
}
