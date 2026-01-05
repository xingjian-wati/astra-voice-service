package config

import "fmt"

// AccentConfig contains the detailed instruction for an accent
// The instruction includes both the description and implementation details
type AccentConfig struct {
	Instruction string // Detailed prompt instruction (e.g., "🇮🇳 English (STRONG Indian accent): ...")
}

// LanguageNames provides human-readable names for language codes
var LanguageNames = map[string]string{
	"en":  "English",
	"zh":  "中文 (Chinese)",
	"yue": "粤语 (Cantonese)",
	"es":  "Español (Spanish)",
	"fr":  "Français (French)",
	"de":  "Deutsch (German)",
	"ja":  "日本語 (Japanese)",
	"ko":  "한국어 (Korean)",
	"pt":  "Português (Portuguese)",
	"it":  "Italiano (Italian)",
	"ru":  "Русский (Russian)",
	"ar":  "العربية (Arabic)",
	"hi":  "हिन्दी (Hindi)",
	"th":  "ไทย (Thai)",
	"vi":  "Tiếng Việt (Vietnamese)",
}

// Accents contains all accent configurations with both descriptions and instructions
// This unified structure replaces the old separate AccentDescriptions and AccentDetailedInstructions maps
var Accents = map[string]map[string]AccentConfig{
	"en": {
		// Asian English accents
		"india": {
			Instruction: "🇮🇳 English (STRONG Indian accent): EMPHASIZE Indian English pronunciation with PROMINENT retroflex consonants (especially t, d, n), DISTINCTIVE rolling 'r' sounds, characteristic head-wobble intonation patterns, CLEAR syllable stress differences. Pronounce 'th' as 't/d' (e.g., 'tank you' not 'thank you'), pronounce 'v/w' distinctly (e.g., 'very vell'). Use AUTHENTIC Indian rhythm and melody in speech. Natural Hindi/regional language code-mixing is encouraged.",
		},
		"singapore": {
			Instruction: "🇸🇬 English (STRONG Singaporean Singlish accent): HEAVILY emphasize Singlish patterns - MUST use particles 'lah', 'lor', 'leh', 'meh' frequently at sentence ends. STRONG staccato rhythm, CLIPPED endings, RISING intonation at end of statements. Pronounce consonant clusters simply (e.g., 'las time' not 'last time'), use DISTINCTIVE Singlish grammar patterns. Make the accent VERY recognizable and authentic.",
		},
		"malaysia": {
			Instruction: "🇲🇾 English (STRONG Malaysian accent): EMPHASIZE Malaysian English with PROMINENT musical intonation, CLIPPED word endings, frequent Malay/Chinese phrase mixing. Use 'lah' particles, STRONG stress patterns, distinctive vowel pronunciations. Pronounce 'th' as 't' or 'd'. Make the accent CLEARLY Malaysian, not neutral.",
		},
		"philippines": {
			Instruction: "🇵🇭 English (STRONG Filipino accent): Use PROMINENT Filipino accent with CLEAR Tagalog influence - soft 'f' sounds (often like 'p'), DISTINCTIVE 'r' pronunciation, characteristic rising-falling intonation. Use Filipino expressions naturally, EMPHASIZE the melodic speech pattern typical of Filipino English.",
		},
		"hongkong": {
			Instruction: "🇭🇰 English (STRONG Hong Kong accent): HEAVILY emphasize Cantonese-influenced pronunciation - PROMINENT tone-like intonation from Cantonese, DISTINCTIVE final consonant handling, frequent Cantonese code-switching. 'R' and 'L' distinctions may blur, 'th' becomes 't/d'. Make the accent UNMISTAKABLY Hong Kong style.",
		},

		// Western English accents
		"us": {
			Instruction: "🇺🇸 English (CLEAR American accent): Use STRONG General American accent with PRONOUNCED rhotic 'r' sounds everywhere (especially at word ends), FLAT 'a' sounds (e.g., 'cAAn' not 'cahn'), characteristic American 't' flapping between vowels (e.g., 'budder' for 'butter'). EMPHASIZE typical American rhythm and intonation patterns.",
		},
		"uk": {
			Instruction: "🇬🇧 English (STRONG British RP accent): Use DISTINCTIVE Received Pronunciation with CLEAR non-rhotic 'r' (dropped at word ends), PROMINENT long 'a' sounds (e.g., 'bahth' not 'bath'), CRISP 't' sounds never flapped. EMPHASIZE British vowel distinctions and characteristic clipped rhythm.",
		},
		"australia": {
			Instruction: "🇦🇺 English (STRONG Australian accent): HEAVILY emphasize Aussie vowel shifts - 'day' sounds like 'die', 'I' sounds like 'oi', PROMINENT rising intonation (statements sound like questions). Use AUTHENTIC Aussie slang frequently ('mate', 'yeah nah', 'no worries'). Make it sound DISTINCTLY Australian.",
		},
		"newzealand": {
			Instruction: "🇳🇿 English (STRONG Kiwi accent): EMPHASIZE distinctive NZ vowel shifts - 'bed' sounds like 'bid', 'fish and chips' sounds like 'fush and chups', PROMINENT short 'i' pronunciation. Use CLEAR Kiwi intonation patterns and expressions ('yeah nah', 'sweet as'). Make it UNMISTAKABLY New Zealand.",
		},
		"canada": {
			Instruction: "🇨🇦 English (STRONG Canadian accent): EMPHASIZE Canadian vowel raising - 'about' and 'out' with DISTINCTIVE raised diphthongs (sounds like 'aboot'), characteristic Canadian 'eh?' usage. CLEAR differences from American accent while maintaining Canadian identity.",
		},
		"ireland": {
			Instruction: "🇮🇪 English (STRONG Irish accent): Use PROMINENT Irish lilt with DISTINCTIVE melodic rise and fall, CLEAR rhotic 'r' sounds, characteristic 'th' pronounced as 't' or 'd'. Use AUTHENTIC Irish expressions and rhythm patterns. Make it sound GENUINELY Irish, not British.",
		},
		"scotland": {
			Instruction: "🏴󠁧󠁢󠁳󠁣󠁴󠁿 English (STRONG Scottish accent): Use HEAVILY rolled 'r' sounds throughout, DISTINCTIVE Scottish vowel sounds ('out' like 'oot', 'down' like 'doon'), characteristic glottal stops. EMPHASIZE the distinctive Scottish rhythm and intonation. Make it CLEARLY Scottish, not generic British.",
		},

		// African English accents
		"southafrica": {
			Instruction: "🇿🇦 English (STRONG South African accent): EMPHASIZE distinctive SA vowel sounds - 'i' sounds more like 'u' ('pit' like 'put'), CLEAR Afrikaans influence in pronunciation and rhythm. Use AUTHENTIC South African expressions. Make the accent DISTINCTLY South African.",
		},
		"nigeria": {
			Instruction: "🇳🇬 English (STRONG Nigerian accent): Use PROMINENT Nigerian rhythm with CLEAR syllable timing, DISTINCTIVE pitch patterns from tonal language influence. Natural incorporation of Pidgin expressions. EMPHASIZE the characteristic Nigerian intonation and melody.",
		},
		"kenya": {
			Instruction: "🇰🇪 English (STRONG Kenyan accent): CLEAR East African accent with DISTINCTIVE Swahili influence, PROMINENT syllable clarity, characteristic British-influenced but distinctly Kenyan pronunciation patterns.",
		},

		// American regional accents
		"southern": {
			Instruction: "🇺🇸 English (STRONG Southern drawl): HEAVILY emphasize Southern drawl with PROLONGED vowels, DISTINCTIVE 'r' dropping before consonants, characteristic 'i' pronunciation ('nice' like 'nahs'). Use AUTHENTIC Southern expressions ('y'all', 'fixing to', 'bless your heart'). Make it sound GENUINELY Southern.",
		},
		"newyork": {
			Instruction: "🗽 English (STRONG New York accent): EMPHASIZE classic NYC pronunciation - 'coffee' like 'cawfee', 'talk' like 'tawk', DISTINCTIVE 'r' dropping, characteristic vowel sounds. Use AUTHENTIC NYC expressions and rhythm. Make it UNMISTAKABLY New York.",
		},
		"boston": {
			Instruction: "🇺🇸 English (STRONG Boston accent): HEAVILY emphasize non-rhotic 'r' dropping ('park the car' becomes 'pahk the cah'), DISTINCTIVE broad 'a' sounds, characteristic Boston vowels. Make it sound CLEARLY Boston, very recognizable.",
		},

		// British regional accents
		"london": {
			Instruction: "🇬🇧 English (STRONG Cockney/London accent): EMPHASIZE glottal stops (replacing 't' sounds), DISTINCTIVE vowel shifts, 'th' pronounced as 'f' or 'v' ('think' like 'fink'), characteristic rhyming slang usage. Make it GENUINELY Cockney.",
		},
		"liverpool": {
			Instruction: "🇬🇧 English (STRONG Scouse accent): HEAVILY emphasize Liverpool's DISTINCTIVE nasal quality, characteristic 'k' sounds at back of throat, PROMINENT Scouse vowel sounds. Use AUTHENTIC Scouse expressions. Make it UNMISTAKABLY Liverpool.",
		},
		"manchester": {
			Instruction: "🇬🇧 English (STRONG Manchester accent): EMPHASIZE flat Northern vowels, DISTINCTIVE short 'a' sounds, characteristic Manchester rhythm and glottal stops. Make it sound CLEARLY Mancunian, not generic Northern.",
		},
	},

	"zh": {
		"mainland": {
			Instruction: "🇨🇳 中文（标准普通话口音 - 强化）：使用标准的北京普通话发音，强调卷舌音（zh、ch、sh、r），清晰的四声调，标准的儿化音。展现纯正的大陆普通话特色，发音清晰标准。",
		},
		"taiwan": {
			Instruction: "🇹🇼 中文（台湾口音 - 强化）：使用台湾国语，强调轻声和语气词的使用，卷舌音较轻或不卷舌（如「是」读作「西」），语调较为柔和温和。使用台湾特有的用词如「很棒」、「哦」等。让口音明显带有台湾特色。",
		},
		"singapore": {
			Instruction: "🇸🇬 中文（新加坡口音 - 强化）：使用新加坡华语，语速较快，带有明显的闽南语或粤语影响，卷舌音弱化，常夹杂英语单词。使用新加坡特有的表达方式，让口音清晰可辨。",
		},
	},

	"yue": {
		"hongkong": {
			Instruction: "🇭🇰 粤语（香港口音 - 强化）：使用地道的香港粤语，强调懒音特征，清晰的九声六调，频繁使用「啦」「囉」「咩」等语气助词。展现纯正港式粤语的韵味和节奏感。",
		},
		"guangdong": {
			Instruction: "🇨🇳 粤语（广东口音 - 强化）：使用广东省粤语，保持标准粤语发音，声调较香港更标准，少用懒音。使用广东地区特有的俗语和表达方式。",
		},
	},

	"es": {
		"spain": {
			Instruction: "🇪🇸 Español (acento español FUERTE): Use STRONG Castilian Spanish with PROMINENT 'th' sound (ceceo) for 'c' and 'z' (very distinctive), CLEAR distinction between 's' and 'th' sounds, characteristic Spanish 'r' and 'rr' pronunciation. Make it sound GENUINELY from Spain.",
		},
		"mexico": {
			Instruction: "🇲🇽 Español (acento mexicano FUERTE): Use STRONG Mexican Spanish pronunciation with DISTINCTIVE soft consonants, characteristic Mexican intonation patterns, AUTHENTIC Mexican expressions and vocabulary. Make it sound CLEARLY Mexican, not generic Spanish.",
		},
		"latin": {
			Instruction: "🌎 Español (acento latinoamericano FUERTE): Use STRONG Latin American Spanish patterns with CLEAR 's' pronunciation (no 'th' sound), characteristic Latin American rhythm and melody. EMPHASIZE the distinctive features of Latin American Spanish.",
		},
	},
}

// GetLanguageName returns the human-readable name for a language code
func GetLanguageName(langCode string) string {
	if name, ok := LanguageNames[langCode]; ok {
		return name
	}
	return langCode
}

// GetAccentDetailedInstruction returns detailed accent instruction for prompt generation
func GetAccentDetailedInstruction(langCode, accentCode string) string {
	if langAccents, ok := Accents[langCode]; ok {
		if accent, ok := langAccents[accentCode]; ok {
			return accent.Instruction
		}
	}
	// Fallback to generic description
	langName := GetLanguageName(langCode)
	return fmt.Sprintf("🌐 %s (%s accent/dialect): Use %s accent pronunciation, intonation, and speaking patterns", langName, accentCode, accentCode)
}

// IsValidAccent checks if an accent code is valid for a given language
func IsValidAccent(langCode, accentCode string) bool {
	if langAccents, ok := Accents[langCode]; ok {
		_, exists := langAccents[accentCode]
		return exists
	}
	return false
}

// GetAvailableAccents returns all available accent codes for a language
func GetAvailableAccents(langCode string) []string {
	if langAccents, ok := Accents[langCode]; ok {
		accents := make([]string, 0, len(langAccents))
		for code := range langAccents {
			accents = append(accents, code)
		}
		return accents
	}
	return []string{}
}
