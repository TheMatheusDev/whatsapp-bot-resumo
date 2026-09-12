package utils

import (
	"strconv"
	"strings"

	wstypes "whatsapp-summarizer/src/types"
)

// ParseSummarizeOptions parses command-line arguments and returns SummarizeOptions
// with the parsed style and personality settings.
//
// Parameters:
//   - args: slice of string arguments to parse
//   - includeNonFlags: if true, non-flag arguments are collected and returned separately
//
// Returns:
//   - style: the parsed style option (short, medium, or long)
//   - personality: the parsed personality option
//   - nonFlagArgs: remaining arguments that are not flags (only if includeNonFlags is true)
func ParseSummarizeOptions(args []string, includeNonFlags bool) (style string, personality string, nonFlagArgs []string) {
	// Set defaults
	style = "short"
	personality = "resumobot"

	for _, arg := range args {
		argLower := strings.ToLower(arg)
		switch argLower {
		case "--curto", "--c", "-curto", "-c":
			style = "short"
		case "--medio", "--m", "-medio", "-m":
			style = "medium"
		case "--longo", "--l", "-longo", "-l":
			style = "long"
		case "--clt", "-clt":
			personality = "clt"
		case "--farialimer", "--fl", "-farialimer", "-fl":
			personality = "farialimer"
		case "--zoomer", "--z", "-zoomer", "-z":
			personality = "zoomer"
		case "--profeta", "--pft", "-profeta", "-pft":
			personality = "profeta"
		case "--resumobot", "--rb", "-resumobot", "-rb":
			personality = "resumobot"
		default:
			// Not a recognized flag
			if includeNonFlags {
				nonFlagArgs = append(nonFlagArgs, arg)
			}
		}
	}

	return style, personality, nonFlagArgs
}

// ParseSummarizeArgs parses command arguments into count, style, personality, question, and whether an explicit count was provided.
// Flags (e.g. --clt, --longo) are extracted regardless of their position in the arguments.
// If the first non-flag argument is a valid integer, it is treated as the message count.
// Otherwise, defaultCount is used, and all non-flag arguments are joined to form the question.
func ParseSummarizeArgs(args []string, defaultCount int) (count int, style string, personality string, question string, hasExplicitCount bool) {
	style, personality, nonFlags := ParseSummarizeOptions(args, true)

	if len(nonFlags) == 0 {
		return defaultCount, style, personality, "", false
	}

	if n, err := strconv.Atoi(nonFlags[0]); err == nil {
		return n, style, personality, strings.Join(nonFlags[1:], " "), true
	}

	return defaultCount, style, personality, strings.Join(nonFlags, " "), false
}

// ParseSummarizeOptionsToStruct is a convenience function that returns a SummarizeOptions struct.
// Any non-flag arguments are joined as the Question.
func ParseSummarizeOptionsToStruct(args []string, count int) wstypes.SummarizeOptions {
	style, personality, nonFlags := ParseSummarizeOptions(args, true)
	return wstypes.SummarizeOptions{
		Count:       count,
		Style:       style,
		Personality: personality,
		Question:    strings.Join(nonFlags, " "),
	}
}
