package embed

import "math"

func CosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	dot := 0.0
	magA := 0.0
	magB := 0.0
	for i := range a {
		dot += a[i] * b[i]
		magA += a[i] * a[i]
		magB += b[i] * b[i]
	}
	denom := math.Sqrt(magA) * math.Sqrt(magB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

func DotProduct(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}
	sum := 0.0
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

func CombineEmbeddings(embeddings []*SynsetEmbedding, weights []float64) []float64 {
	if len(embeddings) == 0 {
		return nil
	}
	dim := len(embeddings[0].Vector)
	result := make([]float64, dim)
	totalWeight := 0.0

	for i, emb := range embeddings {
		w := 1.0
		if weights != nil && i < len(weights) {
			w = weights[i]
		}
		totalWeight += w
		for j, v := range emb.Vector {
			result[j] += v * w
		}
	}

	if totalWeight > 0 {
		for j := range result {
			result[j] /= totalWeight
		}
	}
	return result
}

func ScaleVector(v []float64, s float64) []float64 {
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = x * s
	}
	return out
}

func Normalize(v []float64) []float64 {
	mag := 0.0
	for _, x := range v {
		mag += x * x
	}
	mag = math.Sqrt(mag)
	out := make([]float64, len(v))
	if mag > 0 {
		for i, x := range v {
			out[i] = x / mag
		}
	}
	return out
}
