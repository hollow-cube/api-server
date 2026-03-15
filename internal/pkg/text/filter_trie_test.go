package text

import "testing"

func getTestTrie() *FilterTrie {
	trie := &FilterTrie{}
	trie.Put("shit", "horseshit", "bullshit")
	trie.Put("fuck")
	return trie
}

func TestPositiveProfanities(t *testing.T) {
	trie := getTestTrie()
	texts := map[string]string{
		"this shit":   "shit",
		"fuck this":   "fuck",
		"hello world": "",
	}

	for text, expected := range texts {
		result := trie.Test(text)
		if result == nil && len(expected) != 0 {
			t.Errorf("Expected '%s' but got nil from '%s'", expected, text)
		} else if result != nil && expected != *result {
			t.Errorf("Expected '%s' but got '%s' from '%s'", expected, *result, text)
		}
	}
}

func TestNegativeProfanities(t *testing.T) {
	trie := getTestTrie()
	texts := map[string]string{
		"this horseshit":     "",
		"fuck this bullshit": "fuck",
		"hello world":        "",
	}

	for text, expected := range texts {
		result := trie.Test(text)
		if result == nil && len(expected) != 0 {
			t.Errorf("Expected '%s' but got nil from '%s'", expected, text)
		} else if result != nil && expected != *result {
			t.Errorf("Expected '%s' but got '%s' from '%s'", expected, *result, text)
		}
	}
}

func TestLongText(t *testing.T) {
	trie := getTestTrie()
	// an excerpt from romeo and juliet, copyright: public domain
	text := `
		PRINCE.
		Rebellious subjects, enemies to peace,
		Profaners of this neighbour-stained steel,—
		Will they not hear? What, ho! You men, you beasts,
		That quench the fire of your pernicious rage
		With purple fountains issuing from your veins,
		On pain of torture, from those bloody hands
		Throw your mistemper’d weapons to the ground
		And hear the sentence of your moved prince.
		Three civil brawls, bred of an airy word,
		By thee, old Capulet, and Montague,
		Have thrice disturb’d the quiet of our streets,
		And made Verona’s ancient citizens
		Cast by their grave beseeming ornaments,
		To wield old partisans, in hands as old,
		Canker’d with peace, to part your canker’d hate.
		If ever you disturb our streets again,
		Your lives shall pay the forfeit of the peace.
		For this time all the rest depart away:
		You, Capulet, shall go along with me,
		And Montague, come you this afternoon,
		To know our farther pleasure in this case,
		To old Free-town, our common judgement-place.
		Once more, on pain of death, all men depart.
		
		 [_Exeunt Prince and Attendants; Capulet, Lady Capulet, Tybalt,
		 Citizens and Servants._]
		
		MONTAGUE.
		Who set this ancient quarrel new abroach?
		Speak, nephew, were you by when it began?
		
		BENVOLIO.
		Here were the servants of your adversary
		And yours, close fighting ere I did approach.
		I drew to part them, in the instant came
		The fiery Tybalt, with his sword prepar’d,
		Which, as he breath’d defiance to my ears,
		He swung about his head, and cut the winds,
		Who nothing hurt withal, hiss’d him in scorn.
		While we were interchanging thrusts and blows
		Came more and more, and fought on part and part,
		Till the Prince came, who parted either part.
		
		LADY MONTAGUE.
		O where is Romeo, saw you him today?
		Right glad I am he was not at this fray.
		
		BENVOLIO.
		Madam, an hour before the worshipp’d sun
		Peer’d forth the golden window of the east,
		A troubled mind drave me to walk abroad,
		Where underneath the grove of sycamore
		That westward rooteth from this city side,
		So early walking did I see your son.
		Towards him I made, but he was ware of me,
		And stole into the covert of the wood.
		I, measuring his affections by my own,
		Which then most sought where most might not be found,
		Being one too many by my weary self,
		Pursu’d my humour, not pursuing his,
		And gladly shunn’d who gladly fled from me.
		
		MONTAGUE.
		Many a morning hath he there been seen,
		With tears augmenting the fresh morning’s dew,
		Adding to clouds more clouds with his deep sighs;
		But all so soon as the all-cheering sun
		Should in the farthest east begin to draw
		The shady curtains from Aurora’s bed,
		Away from light steals home my heavy son,
		And private in his chamber pens himself,
		Shuts up his windows, locks fair daylight out
		And makes himself an artificial night.
		Black and portentous must this humour prove,
		Unless good counsel may the cause remove.
		
		BENVOLIO.
		My noble uncle, do you know the cause?
		
		MONTAGUE.
		I neither know it nor can learn of him.
		
		BENVOLIO.
		Have you importun’d him by any means?
		
		MONTAGUE.
		Both by myself and many other friends;
		But he, his own affections’ counsellor,
		Is to himself—I will not say how true—
		But to himself so secret and so close,
		So far from sounding and discovery,
		As is the bud bit with an envious worm
		Ere he can spread his sweet leaves to the air,
		Or dedicate his beauty to the sun.
		Could we but learn from whence his sorrows grow,
		We would as willingly give cure as know.
		`

	result := trie.Test(text)
	if result != nil {
		t.Errorf("Expected no profanity but got '%s'", *result)
	}
}
