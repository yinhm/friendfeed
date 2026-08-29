package server

import "testing"

func TestLegacyYouTubeVideoIDExtractsArchivedPlayer(t *testing.T) {
	if got := legacyYouTubeVideoIDFromURL("http://www.youtube.com/v/nJDf-sdylwU&autoplay=1"); got != "nJDf-sdylwU" {
		t.Fatalf("direct video ID=%q", got)
	}
	player := `<object width="425" height="350"><param name="movie" value="http://www.youtube.com/v/nJDf-sdylwU&amp;autoplay=1&amp;showsearch=0&amp;fs=1"></param><param name="allowFullScreen" value="true"><embed src="http://www.youtube.com/v/nJDf-sdylwU&amp;autoplay=1" type="application/x-shockwave-flash"></embed></object>`
	if got := legacyYouTubeVideoID(player); got != "nJDf-sdylwU" {
		t.Fatalf("video ID=%q", got)
	}
}

func TestLegacyYouTubeVideoIDRejectsUntrustedPlayers(t *testing.T) {
	for _, player := range []string{
		`<iframe src="https://evil.example/v/nJDf-sdylwU"></iframe>`,
		`<embed src="javascript:alert(1)">`,
		`<embed src="https://youtube.com.evil.example/v/nJDf-sdylwU">`,
		`<embed src="https://www.youtube.com/embed/nJDf-sdylwU">`,
		`<embed src="https://www.youtube.com/v/not-valid">`,
	} {
		if got := legacyYouTubeVideoID(player); got != "" {
			t.Fatalf("accepted player %q as %q", player, got)
		}
	}
}
