package portable

import (
	"reflect"
	"testing"

	"golang.org/x/text/unicode/norm"
)

func TestKeyNormalizesCaseAndUnicode(t *testing.T) {
	if got, want := Key("あとか\u3099たり"), Key("あとがたり"); got != want {
		t.Fatalf("Key(NFD)=%q, want NFC key %q", got, want)
	}
	if got, want := Key("GAME"), Key("game"); got != want {
		t.Fatalf("Key uppercase=%q, want %q", got, want)
	}
}

func TestMutationCandidatesCoverNFCAndNFDOnce(t *testing.T) {
	value := "/target/あとか\u3099たり.m3u"
	want := []string{norm.NFC.String(value), norm.NFD.String(value)}
	if got := MutationCandidates(value); !reflect.DeepEqual(got, want) {
		t.Fatalf("MutationCandidates=%#v, want %#v", got, want)
	}
}

func TestIsAppleDouble(t *testing.T) {
	for _, value := range []string{"._song.m4a", "Library/A/._AlbumArt.jpg", `Library\A\._song.m4a`} {
		if !IsAppleDouble(value) {
			t.Errorf("IsAppleDouble(%q)=false", value)
		}
	}
	if IsAppleDouble("Library/A/song.m4a") {
		t.Fatal("normal audio was treated as AppleDouble")
	}
}

func TestAndroidVisibleMatchesMacExFATEncoding(t *testing.T) {
	logical := `Library/チト(CV:水瀬いのり)/Grimgar "BEST"/song?.m4a`
	want := "Library/チト(CV\uf022水瀬いのり)/Grimgar \uf020BEST\uf020/song\uf025.m4a"
	if got := AndroidVisible(logical); got != want {
		t.Fatalf("AndroidVisible=%q, want %q", got, want)
	}
	if got := LogicalFromAndroid(want); got != logical {
		t.Fatalf("LogicalFromAndroid=%q, want %q", got, logical)
	}
}

func TestAndroidVisibleIsIdempotent(t *testing.T) {
	value := "Library/A\uf022B/Album \uf020Best\uf020/song.m4a"
	if got := AndroidVisible(AndroidVisible(value)); got != value {
		t.Fatalf("encoded path changed: %q", got)
	}
}

func TestAndroidVisibleEncodesTrailingSpaceAndDotPerComponent(t *testing.T) {
	logical := "Library/BLUE REFLECTION TIE / 帝/Album./song.m4a"
	want := "Library/BLUE REFLECTION TIE\uf028/ 帝/Album\uf029/song.m4a"
	if got := AndroidVisible(logical); got != want {
		t.Fatalf("AndroidVisible=%q, want %q", got, want)
	}
	if got := LogicalFromAndroid(want); got != logical {
		t.Fatalf("LogicalFromAndroid=%q, want %q", got, logical)
	}
}

func TestAndroidVisibleRoundTripsEveryMacReservedCharacter(t *testing.T) {
	tests := []struct {
		logical rune
		encoded rune
	}{
		{0x01, '\uf001'}, {'"', '\uf020'}, {'*', '\uf021'}, {':', '\uf022'},
		{'<', '\uf023'}, {'>', '\uf024'}, {'?', '\uf025'}, {'\\', '\uf026'},
		{'|', '\uf027'}, {0x7f, '\uf07f'},
	}
	for _, test := range tests {
		logical := "Library/A" + string(test.logical) + "B/song.m4a"
		encoded := "Library/A" + string(test.encoded) + "B/song.m4a"
		if got := AndroidVisible(logical); got != encoded {
			t.Errorf("encode U+%04X=%q, want %q", test.logical, got, encoded)
		}
		if got := LogicalFromAndroid(encoded); got != logical {
			t.Errorf("decode U+%04X=%q, want %q", test.encoded, got, logical)
		}
	}
}
