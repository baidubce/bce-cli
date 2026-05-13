package suggester

import "sort"

type candidate struct {
	name     string
	distance int
}

// SuggestCommand returns up to 3 commands from available that are within
// edit distance 3 of input, ordered by ascending distance.
func SuggestCommand(input string, available []string) []string {
	var candidates []candidate
	for _, cmd := range available {
		dist := levenshteinDistance(input, cmd)
		if dist <= 3 {
			candidates = append(candidates, candidate{cmd, dist})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].distance < candidates[j].distance
	})
	return topN(candidates, 3)
}

func topN(candidates []candidate, n int) []string {
	if len(candidates) < n {
		n = len(candidates)
	}
	result := make([]string, n)
	for i := range result {
		result[i] = candidates[i].name
	}
	return result
}

func levenshteinDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	dp := make([][]int, la+1)
	for i := range dp {
		dp[i] = make([]int, lb+1)
		dp[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		dp[0][j] = j
	}

	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			if ra[i-1] == rb[j-1] {
				dp[i][j] = dp[i-1][j-1]
			} else {
				dp[i][j] = 1 + min3(dp[i-1][j], dp[i][j-1], dp[i-1][j-1])
			}
		}
	}
	return dp[la][lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
