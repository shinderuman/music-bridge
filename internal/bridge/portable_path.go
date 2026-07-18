package bridge

import (
	"os"
	"path"
	"strings"

	"golang.org/x/text/unicode/norm"
)

func portablePathKey(value string) string {
	return norm.NFC.String(strings.ToLower(logicalPathFromAndroid(value)))
}

// exFAT上のUnicodeファイル名はos.ReadDirではNFDに見えても、変更操作では
// NFCまたはNFDの片方だけを受け付けることがある。変更対象の解決を一か所に集約する。
func portableMutationPath(value string) string {
	for _, candidate := range portableMutationCandidates(value) {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return value
}

func removePortablePath(value string) error {
	for _, candidate := range portableMutationCandidates(value) {
		if err := os.Remove(candidate); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func portableMutationCandidates(value string) []string {
	candidates := []string{
		norm.NFC.String(value),
		value,
		norm.NFD.String(value),
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if !seen[candidate] {
			seen[candidate] = true
			result = append(result, candidate)
		}
	}
	return result
}

func isAppleDoublePath(value string) bool {
	return strings.HasPrefix(path.Base(strings.ReplaceAll(value, "\\", "/")), "._")
}

// macOSはFAT/exFATで使えない文字をApple互換の私用文字へ変換して保存する。
// Androidから同じボリュームを見ると変換後の文字がそのまま見えるため、
// Androidへ直接同期するパスとM3U内の参照も同じ表現へ揃える。
func androidVisiblePath(value string) string {
	components := strings.Split(value, "/")
	for componentIndex, component := range components {
		characters := []rune(component)
		for index, character := range characters {
			if character >= 0 && character <= 0x1f {
				characters[index] = '\uf000' + character
				continue
			}
			switch character {
			case '"':
				characters[index] = '\uf020'
			case '*':
				characters[index] = '\uf021'
			case ':':
				characters[index] = '\uf022'
			case '<':
				characters[index] = '\uf023'
			case '>':
				characters[index] = '\uf024'
			case '?':
				characters[index] = '\uf025'
			case '\\':
				characters[index] = '\uf026'
			case '|':
				characters[index] = '\uf027'
			case 0x7f:
				characters[index] = '\uf07f'
			}
		}
		for index := len(characters) - 1; index >= 0; index-- {
			if characters[index] == ' ' {
				characters[index] = '\uf028'
			} else if characters[index] == '.' {
				characters[index] = '\uf029'
			} else {
				break
			}
		}
		components[componentIndex] = string(characters)
	}
	return strings.Join(components, "/")
}

func logicalPathFromAndroid(value string) string {
	return strings.Map(func(character rune) rune {
		if character >= '\uf000' && character <= '\uf01f' {
			return character - '\uf000'
		}
		switch character {
		case '\uf020':
			return '"'
		case '\uf021':
			return '*'
		case '\uf022':
			return ':'
		case '\uf023':
			return '<'
		case '\uf024':
			return '>'
		case '\uf025':
			return '?'
		case '\uf026':
			return '\\'
		case '\uf027':
			return '|'
		case '\uf028':
			return ' '
		case '\uf029':
			return '.'
		case '\uf07f':
			return 0x7f
		default:
			return character
		}
	}, value)
}
