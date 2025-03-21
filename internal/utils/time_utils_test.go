package utils

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestZeroTime(t *testing.T) {
	expected := time.Time{}
	actual := ZeroTime()

	if !actual.Equal(expected) {
		t.Errorf("Expected %v, got %v", expected, actual)
	}
}

func TestNow(t *testing.T) {
	now := Now()
	if now.Location() != time.UTC {
		t.Errorf("Now() should be in UTC, but got %s", now.Location())
	}

	if time.Since(now) > time.Second {
		t.Errorf("Now() is not returning the current time")
	}
}

func TestNowPtr(t *testing.T) {
	nowPtr := NowPtr()
	if nowPtr == nil {
		t.Fatal("NowPtr() returned nil")
	}

	if nowPtr.Location() != time.UTC {
		t.Errorf("NowPtr() should be in UTC, but got %s", nowPtr.Location())
	}

	if time.Since(*nowPtr) > time.Second {
		t.Errorf("NowPtr() is not returning the current time")
	}
}

func TestConvertTimeToTimestampPtr(t *testing.T) {
	// Test with a non-nil time
	testTime := time.Now()
	result := ConvertTimeToTimestampPtr(&testTime)
	if result == nil {
		t.Fatal("ConvertTimeToTimestampPtr returned nil for non-nil input")
	}
	if result.Seconds != testTime.Unix() {
		t.Errorf("Expected seconds %v, got %v", testTime.Unix(), result.Seconds)
	}
	if result.Nanos != int32(testTime.Nanosecond()) {
		t.Errorf("Expected nanos %v, got %v", testTime.Nanosecond(), result.Nanos)
	}

	// Test with a nil time
	resultNil := ConvertTimeToTimestampPtr(nil)
	if resultNil != nil {
		t.Fatal("ConvertTimeToTimestampPtr should return nil for nil input")
	}
}

func TestToDatePtr(t *testing.T) {
	// Test with a non-nil time
	now := time.Now()
	datePtr := ToDatePtr(&now)
	if datePtr == nil {
		t.Fatal("ToDatePtr returned nil for non-nil input")
	}
	if !datePtr.Equal(now.Truncate(24 * time.Hour).UTC()) {
		t.Errorf("Expected %v, got %v", now.Truncate(24*time.Hour).UTC(), *datePtr)
	}

	// Test with a nil time
	nilDatePtr := ToDatePtr(nil)
	if nilDatePtr != nil {
		t.Fatal("ToDatePtr should return nil for nil input")
	}
}

func TestUnmarshalDateTime(t *testing.T) {
	customLayout1 := "2006-01-02 15:04:05"
	customLayout2 := "2006-01-02T15:04:05.000-0700"
	customLayout3 := "2006-01-02T15:04:05-07:00"

	// Test valid RFC3339 input
	rfc3339Input := "2006-01-02T15:04:05Z"
	dt, err := UnmarshalDateTime(rfc3339Input)
	if err != nil {
		t.Errorf("UnmarshalDateTime returned an error for valid RFC3339 input: %v", err)
	}
	if dt == nil || dt.Format(time.RFC3339) != rfc3339Input {
		t.Errorf("Expected %s, got %v", rfc3339Input, dt)
	}

	// Test with custom layout 1
	custom1Input := "2006-01-02 15:04:05"
	custom1Dt, custom1Err := UnmarshalDateTime(custom1Input)
	if custom1Err != nil || custom1Dt == nil || custom1Dt.Format(customLayout1) != custom1Input {
		t.Errorf("UnmarshalDateTime failed for custom layout 1: %v", custom1Err)
	}

	// Test with custom layout 2
	custom2Input := "2006-01-02T15:04:05.000-0700"
	custom2Dt, custom2Err := UnmarshalDateTime(custom2Input)
	if custom2Err != nil || custom2Dt == nil || custom2Dt.Format(customLayout2) != custom2Input {
		t.Errorf("UnmarshalDateTime failed for custom layout 2: %v", custom2Err)
	}

	// Test with custom layout 3
	custom3Input := "2006-01-02T15:04:05-07:00"
	custom3Dt, custom3Err := UnmarshalDateTime(custom3Input)
	if custom3Err != nil || custom3Dt == nil || custom3Dt.Format(customLayout3) != custom3Input {
		t.Errorf("UnmarshalDateTime failed for custom layout 3: %v", custom3Err)
	}

	// Test with empty input
	emptyDt, emptyErr := UnmarshalDateTime("")
	if emptyErr != nil || emptyDt != nil {
		t.Errorf("Expected nil for empty input, got %v and error %v", emptyDt, emptyErr)
	}

	// Test with invalid input
	invalidInput := "invalid-date"
	invalidDt, invalidErr := UnmarshalDateTime(invalidInput)
	if invalidErr == nil {
		t.Errorf("Expected error for invalid input, got %v", invalidDt)
	}
}

func TestTimestampProtoToTimePtr(t *testing.T) {
	// Test with a non-nil timestamp
	testTimestamp := timestamppb.New(time.Now())
	result := TimestampProtoToTimePtr(testTimestamp)
	if result == nil {
		t.Fatal("TimestampProtoToTimePtr returned nil for non-nil input")
	}
	if !result.Equal(testTimestamp.AsTime()) {
		t.Errorf("Expected %v, got %v", testTimestamp.AsTime(), *result)
	}

	// Test with a nil timestamp
	resultNil := TimestampProtoToTimePtr(nil)
	if resultNil != nil {
		t.Fatal("TimestampProtoToTimePtr should return nil for nil input")
	}
}

func TestIsEqualTimePtr(t *testing.T) {
	now := time.Now()

	// Both pointers are nil
	if !IsEqualTimePtr(nil, nil) {
		t.Error("IsEqualTimePtr should return true for two nil pointers")
	}

	// One pointer is nil, the other is not
	if IsEqualTimePtr(&now, nil) {
		t.Error("IsEqualTimePtr should return false when only one pointer is nil")
	}
	if IsEqualTimePtr(nil, &now) {
		t.Error("IsEqualTimePtr should return false when only one pointer is nil")
	}

	// Both pointers are non-nil and equal
	timeCopy := now
	if !IsEqualTimePtr(&now, &timeCopy) {
		t.Error("IsEqualTimePtr should return true for pointers to equal times")
	}

	// Both pointers are non-nil and not equal
	differentTime := now.Add(time.Hour)
	if IsEqualTimePtr(&now, &differentTime) {
		t.Error("IsEqualTimePtr should return false for pointers to different times")
	}
}

func TestAddOneMonthFallbackToLastDayOfMonth(t *testing.T) {
	testCases := []struct {
		input    time.Time
		expected time.Time
	}{
		{
			input:    time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC),
			expected: time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC),
		},
		{
			input:    time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			expected: time.Date(2024, time.February, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			input:    time.Date(2024, time.March, 31, 0, 0, 0, 0, time.UTC),
			expected: time.Date(2024, time.April, 30, 0, 0, 0, 0, time.UTC),
		},
		{
			input:    time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC),
			expected: time.Date(2024, time.April, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.input.Format("2006-01-02"), func(t *testing.T) {
			result := AddOneMonthFallbackToLastDayOfMonth(testCase.input)
			if !result.Equal(testCase.expected) {
				t.Errorf("Expected: %s, Got: %s", testCase.expected.Format("2006-01-02"), result.Format("2006-01-02"))
			}
		})
	}
}

func TestGenerateYearMonths(t *testing.T) {
	// Test case 1: January 31, 2023 to March 5, 2024
	start1 := time.Date(2023, time.January, 31, 0, 0, 0, 0, time.UTC)
	end1 := time.Date(2024, time.March, 5, 0, 0, 0, 0, time.UTC)
	expected1 := []YearMonth{
		{Year: 2023, Month: time.January},
		{Year: 2023, Month: time.February},
		{Year: 2023, Month: time.March},
		{Year: 2023, Month: time.April},
		{Year: 2023, Month: time.May},
		{Year: 2023, Month: time.June},
		{Year: 2023, Month: time.July},
		{Year: 2023, Month: time.August},
		{Year: 2023, Month: time.September},
		{Year: 2023, Month: time.October},
		{Year: 2023, Month: time.November},
		{Year: 2023, Month: time.December},
		{Year: 2024, Month: time.January},
		{Year: 2024, Month: time.February},
		{Year: 2024, Month: time.March},
	}

	// Test case 2: March 1, 2022 to April 15, 2022
	start2 := time.Date(2022, time.March, 1, 0, 0, 0, 0, time.UTC)
	end2 := time.Date(2022, time.April, 15, 0, 0, 0, 0, time.UTC)
	expected2 := []YearMonth{
		{Year: 2022, Month: time.March},
		{Year: 2022, Month: time.April},
	}

	// Test case 3: November 15, 2023 to November 16, 2023
	start3 := time.Date(2023, time.November, 15, 0, 0, 0, 0, time.UTC)
	end3 := time.Date(2023, time.November, 16, 0, 0, 0, 0, time.UTC)
	expected3 := []YearMonth{
		{Year: 2023, Month: time.November},
	}

	// Test case 4: December 1, 2024 to December 1, 2024
	start4 := time.Date(2024, time.December, 1, 0, 0, 0, 0, time.UTC)
	end4 := time.Date(2024, time.December, 1, 0, 0, 0, 0, time.UTC)
	expected4 := []YearMonth{
		{Year: 2024, Month: time.December},
	}

	// Test case 5: January 15, 2023 to February 15, 2023
	start5 := time.Date(2023, time.January, 15, 0, 0, 0, 0, time.UTC)
	end5 := time.Date(2023, time.February, 15, 0, 0, 0, 0, time.UTC)
	expected5 := []YearMonth{
		{Year: 2023, Month: time.January},
		{Year: 2023, Month: time.February},
	}

	testCases := []struct {
		start    time.Time
		end      time.Time
		expected []YearMonth
	}{
		{start1, end1, expected1},
		{start2, end2, expected2},
		{start3, end3, expected3},
		{start4, end4, expected4},
		{start5, end5, expected5},
	}

	for _, tc := range testCases {
		t.Run(tc.start.String()+"_"+tc.end.String(), func(t *testing.T) {
			result := GenerateYearMonths(tc.start, tc.end)
			if len(result) != len(tc.expected) {
				t.Errorf("Expected length: %d, Got length: %d", len(tc.expected), len(result))
				return
			}

			for i := range tc.expected {
				if result[i] != tc.expected[i] {
					t.Errorf("Mismatch at index %d, Expected: %+v, Got: %+v", i, tc.expected[i], result[i])
				}
			}
		})
	}
}
