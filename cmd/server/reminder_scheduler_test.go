package main

import "testing"

func TestReminderCheckRecoversAndAllowsNextPass(t *testing.T) {
	runReminderCheck("renewal", func() { panic("provider failure") })
	ran := false
	runReminderCheck("cancellation", func() { ran = true })
	if !ran {
		t.Fatal("next reminder pass was not executed")
	}
}
