package shards

import "sort"

func AssignShards(totalShards int, workers []string) map[string][]int {
	assignment := map[string][]int{}
	if totalShards <= 0 || len(workers) == 0 {
		return assignment
	}

	sorted := append([]string(nil), workers...)
	sort.Strings(sorted)

	for i := 0; i < totalShards; i++ {
		owner := sorted[i%len(sorted)]
		assignment[owner] = append(assignment[owner], i)
	}

	return assignment
}
