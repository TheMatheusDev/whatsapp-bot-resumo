package utils

import (
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
	personality = "clt"

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
		case "--noir", "--detetive", "--d", "-noir", "-detetive", "-d":
			personality = "noir"
		case "--zoomer", "--z", "-zoomer", "-z":
			personality = "zoomer"
		default:
			// Not a recognized flag
			if includeNonFlags {
				nonFlagArgs = append(nonFlagArgs, arg)
			}
		}
	}

	return style, personality, nonFlagArgs
}

// ParseSummarizeOptionsToStruct is a convenience function that returns a SummarizeOptions struct
func ParseSummarizeOptionsToStruct(args []string, count int) wstypes.SummarizeOptions {
	style, personality, _ := ParseSummarizeOptions(args, false)
	return wstypes.SummarizeOptions{
		Count:       count,
		Style:       style,
		Personality: personality,
	}
}
