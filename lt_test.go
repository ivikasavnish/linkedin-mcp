package main

import "testing"

func TestLittleText(t *testing.T) {
	got := littleText("Shipped v2 (finally) #golang #mcp_dev, thanks @team!")
	want := `Shipped v2 \(finally\) {hashtag|\#|golang} {hashtag|\#|mcp\_dev}, thanks \@team!`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestFormatText(t *testing.T) {
	got := formatText("**Hi 2** and *go* but 2*3 stays, #mcp_dev too")
	want := "𝗛𝗶 𝟮 and 𝘨𝘰 but 2*3 stays, #mcp_dev too"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}
