package game

import (
	"fmt"

	"github.com/mentalarena/backend/pkg/rng"
)

type QuestionGenerator struct {
	rng        *rng.SeededRNG
	difficulty int
}

func NewQuestionGenerator(seed int64, difficulty int) *QuestionGenerator {
	if difficulty < 1 {
		difficulty = 1
	}
	if difficulty > 3 {
		difficulty = 3
	}
	return &QuestionGenerator{
		rng:        rng.NewSeededRNG(seed),
		difficulty: difficulty,
	}
}

func (g *QuestionGenerator) Generate(roundNum int) Question {
	opType := g.rng.IntRange(0, 3)

	var a, b, answer int
	var expr string

	switch opType {
	case 0:
		a, b = g.generateAdditionOperands()
		answer = a + b
		expr = fmt.Sprintf("%d + %d", a, b)

	case 1:
		a, b = g.generateSubtractionOperands()
		answer = a - b
		expr = fmt.Sprintf("%d - %d", a, b)

	case 2:
		a, b = g.generateMultiplicationOperands()
		answer = a * b
		expr = fmt.Sprintf("%d × %d", a, b)

	case 3:
		a, b, answer = g.generateDivisionOperands()
		expr = fmt.Sprintf("%d ÷ %d", a, b)
	}

	return Question{
		ID:         roundNum,
		Expression: expr,
		Answer:     answer,
	}
}

func (g *QuestionGenerator) GenerateSequence(totalRounds int) []Question {
	questions := make([]Question, totalRounds)
	for i := 0; i < totalRounds; i++ {
		questions[i] = g.Generate(i + 1)
	}
	return questions
}

func (g *QuestionGenerator) generateAdditionOperands() (int, int) {
	switch g.difficulty {
	case 1:
		return g.rng.IntRange(1, 9), g.rng.IntRange(1, 9)
	case 2:
		return g.rng.IntRange(10, 99), g.rng.IntRange(10, 99)
	case 3:
		return g.rng.IntRange(100, 999), g.rng.IntRange(100, 999)
	default:
		return g.rng.IntRange(10, 99), g.rng.IntRange(10, 99)
	}
}

func (g *QuestionGenerator) generateSubtractionOperands() (int, int) {
	var a, b int
	switch g.difficulty {
	case 1:
		a = g.rng.IntRange(5, 18)
		b = g.rng.IntRange(1, a-1)
	case 2:
		a = g.rng.IntRange(50, 99)
		b = g.rng.IntRange(10, a-1)
	case 3:
		a = g.rng.IntRange(500, 999)
		b = g.rng.IntRange(100, a-1)
	default:
		a = g.rng.IntRange(50, 99)
		b = g.rng.IntRange(10, a-1)
	}
	return a, b
}

func (g *QuestionGenerator) generateMultiplicationOperands() (int, int) {
	switch g.difficulty {
	case 1:
		return g.rng.IntRange(2, 9), g.rng.IntRange(2, 9)
	case 2:
		return g.rng.IntRange(2, 12), g.rng.IntRange(2, 12)
	case 3:
		return g.rng.IntRange(10, 25), g.rng.IntRange(2, 15)
	default:
		return g.rng.IntRange(2, 12), g.rng.IntRange(2, 12)
	}
}

func (g *QuestionGenerator) generateDivisionOperands() (int, int, int) {
	var b, answer int
	switch g.difficulty {
	case 1:
		b = g.rng.IntRange(2, 9)
		answer = g.rng.IntRange(2, 9)
	case 2:
		b = g.rng.IntRange(2, 12)
		answer = g.rng.IntRange(2, 12)
	case 3:
		b = g.rng.IntRange(5, 15)
		answer = g.rng.IntRange(10, 25)
	default:
		b = g.rng.IntRange(2, 12)
		answer = g.rng.IntRange(2, 12)
	}
	a := b * answer
	return a, b, answer
}
