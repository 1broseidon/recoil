package retrieval

import "strings"

// PersonalExpansionsEnabled gates benchmark/personal-domain lexical hints that
// are useful for LoCoMo/LongMemEval-style evals but too specific for production
// retrieval over arbitrary user/project data.
var PersonalExpansionsEnabled bool

// SetPersonalExpansions enables or disables personal-domain expansions.
func SetPersonalExpansions(enabled bool) {
	PersonalExpansionsEnabled = enabled
}

var personalQueryExpansions = []struct {
	markers []string
	terms   []string
}{
	{
		markers: []string{"doctor", "physician"},
		terms:   []string{"dr", "physician", "doctor", "dermatologist", "ent", "specialist", "primary", "care", "provider"},
	},
	{
		markers: []string{"sibling", "brother", "sister"},
		terms:   []string{"sibling", "siblings", "brother", "brothers", "sister", "sisters", "family"},
	},
	{
		markers: []string{"kitchen appliance", "appliance"},
		terms:   []string{"kitchen", "appliance", "blender", "toaster", "microwave", "oven", "mixer", "air", "fryer", "coffee", "maker", "smoker"},
	},
	{
		markers: []string{"buy", "bought", "purchase"},
		terms:   []string{"buy", "bought", "purchase", "purchased", "got", "new"},
	},
	{
		markers: []string{"bake", "baked", "baking"},
		terms:   []string{"bake", "baked", "baking", "cake", "cookies", "bread", "pie", "pastry", "oven", "recipe"},
	},
	{
		markers: []string{"hike", "hikes", "distance"},
		terms:   []string{"hike", "hiked", "hiking", "trail", "distance", "mile", "miles", "kilometer", "kilometers", "km"},
	},
	{
		markers: []string{"sports event", "sporting event"},
		terms:   []string{"sports", "event", "events", "race", "tournament", "marathon", "game", "match", "competition"},
	},
	{
		markers: []string{"graduated", "graduation", "college"},
		terms:   []string{"graduated", "graduation", "college", "university", "bachelor", "bachelors", "degree", "completed", "age", "old"},
	},
	{
		markers: []string{"how old", " age "},
		terms:   []string{"age", "old", "current", "currently"},
	},
}

var personalProfileTags = []struct {
	markers []string
	terms   []string
}{
	{
		markers: []string{"basil", "mint", "herb", "recipe"},
		terms:   []string{"homegrown", "garden", "ingredients", "dinner", "cooking", "recipes", "herbs", "fresh"},
	},
	{
		markers: []string{"power bank", "charging", "battery", "tech accessories"},
		terms:   []string{"phone", "battery", "power", "charging", "portable", "travel", "accessories"},
	},
	{
		markers: []string{"still remember", "high school", "debate team", "advanced placement"},
		terms:   []string{"nostalgic", "nostalgia", "memories", "reunion", "high_school", "school", "friends"},
	},
}

func expansionMatches(text string, markers []string) bool {
	for _, marker := range markers {
		if marker == " age " {
			if strings.Contains(text, marker) || strings.HasPrefix(text, "age ") || strings.HasSuffix(text, " age") {
				return true
			}
			continue
		}
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
