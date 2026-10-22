package main

import "testing"

func TestCurrentPackage(t *testing.T) {
	for _, output := range []string{
		"mResumedActivity: ActivityRecord{aaa u0 com.android.settings/.Settings t9}",
		"topResumedActivity=ActivityRecord{bbb u0 com.example.app/com.example.Main t3}",
		"mCurrentFocus=Window{ccc u0 com.example.app/.Main}",
	} {
		if got, err := currentPackage(output); err != nil || got == "" {
			t.Fatalf("cannot read foreground application from %q: %q %v", output, got, err)
		}
	}
	for _, output := range []string{"", "mResumedActivity: null", "some text com.fake.app/.Main", "mCurrentFocus=Window{u0 invalid;command/.Main}"} {
		if got, err := currentPackage(output); err == nil {
			t.Fatalf("invalid foreground application accepted: %q", got)
		}
	}
}
