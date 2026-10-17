package api_test

import (
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Wrong passwords sent all at once still get only the promised five tries:
// the try is counted before the slow password check, not after it.
func TestLoginLimitHoldsForAParallelBurst(t *testing.T) {
	_, base := newSecurityServer(t, "none", "")
	client := &http.Client{}
	if r := rawPost(t, client, base+"/api/onboarding/admin", goodAdmin, nil); r.StatusCode != http.StatusCreated {
		t.Fatalf("setup: %d", r.StatusCode)
	}
	const burst = 60
	codes := make([]int, burst)
	var wg sync.WaitGroup
	for i := 0; i < burst; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Post(base+"/api/auth/login", "application/json",
				strings.NewReader(`{"username":"ryan","password":"wrong-guess-`+strconv.Itoa(i)+`"}`))
			if err != nil {
				return
			}
			resp.Body.Close()
			codes[i] = resp.StatusCode
		}()
	}
	wg.Wait()
	wrong, limited := 0, 0
	for _, c := range codes {
		switch c {
		case http.StatusUnauthorized:
			wrong++
		case http.StatusTooManyRequests:
			limited++
		}
	}
	if wrong != 5 || limited != burst-5 {
		t.Fatalf("%d got the wrong-password answer and %d were limited, want 5 and %d", wrong, limited, burst-5)
	}
}

// A visitor cannot lock somebody's password check by typing a user name that
// looks like an internal key.
func TestTypedNamesDoNotShareKeysWithPasswordChecks(t *testing.T) {
	_, base := newSecurityServer(t, "", "")
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if r := rawPost(t, client, base+"/api/onboarding/admin", goodAdmin, nil); r.StatusCode != http.StatusCreated {
		t.Fatalf("setup: %d", r.StatusCode)
	}
	// Plenty of wrong sign-ins for the "names" the first account's keys are called, from many addresses.
	for _, name := range []string{"password:1", "code:1"} {
		for i := 0; i < 30; i++ {
			rawPost(t, &http.Client{}, base+"/api/auth/login", map[string]string{"username": name, "password": "x"},
				map[string]string{"X-Forwarded-For": "198.51.100." + strconv.Itoa(i+1)})
		}
	}
	r := rawPost(t, client, base+"/api/auth/change-password", map[string]string{"currentPassword": goodAdmin["password"], "newPassword": "another-long-passphrase-1"}, nil)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("changing the password after those sign-ins: %d, want 200", r.StatusCode)
	}
}
