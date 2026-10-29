package service

import "testing"

func TestChooseSubscriptionInstallmentAmount(t *testing.T) {
	tests := []struct {
		name      string
		remaining float64
		requested float64
		want      float64
		wantErr   bool
	}{
		{name: "caps first payment", remaining: 1305, requested: 0, want: 1000},
		{name: "honors requested first payment", remaining: 1305, requested: 800, want: 800},
		{name: "exact remainder", remaining: 505, requested: 0, want: 505},
		{name: "requested remainder", remaining: 505, requested: 505, want: 505},
		{name: "rejects over remainder", remaining: 505, requested: 506, wantErr: true},
		{name: "accepts one cent tail", remaining: 0.01, requested: 0, want: 0.01},
		{name: "rejects zero remainder", remaining: 0, requested: 0, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := chooseSubscriptionInstallmentAmount(tt.remaining, tt.requested)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v, wantErr=%v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChooseSubscriptionInstallmentAmountRoundsToCents(t *testing.T) {
	got, err := chooseSubscriptionInstallmentAmount(1000.005, 1000.005)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 1000 {
		t.Fatalf("got %.4f, want 1000.00", got)
	}
}

func TestChooseSubscriptionInstallmentAmountSplitsExactTail(t *testing.T) {
	for _, remaining := range []float64{2000, 1000.01, 0.01} {
		got, err := chooseSubscriptionInstallmentAmount(remaining, 0)
		if err != nil {
			t.Fatalf("remaining %.2f: unexpected error: %v", remaining, err)
		}
		want := remaining
		if want > subscriptionInstallmentCap {
			want = subscriptionInstallmentCap
		}
		if got != want {
			t.Fatalf("remaining %.2f: got %.2f, want %.2f", remaining, got, want)
		}
	}
}

func TestInstallmentsLeaveAPayableExactTail(t *testing.T) {
	first, err := chooseSubscriptionInstallmentWithinLimits(1000.01, 0, 1, 1000)
	if err != nil || first != 999.01 {
		t.Fatalf("first=%v, err=%v", first, err)
	}
	last, err := chooseSubscriptionInstallmentWithinLimits(1, 0, 1, 1000)
	if err != nil || last != 1 {
		t.Fatalf("tail=%v, err=%v", last, err)
	}
	if _, err = chooseSubscriptionInstallmentWithinLimits(1000.01, 1000, 1, 1000); err == nil {
		t.Fatal("must not leave a tail below the payment minimum")
	}
}
