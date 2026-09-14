package plain

import (
	"fmt"
	"sync"

	simdjson "github.com/minio/simdjson-go"
)

// simdjson-go parses a document into a tape and stops there: it has no
// Unmarshal, and nothing that fills a struct. What follows walks that tape
// into TwitterStruct and Book by hand, so that the simdjson-go row measures
// the same job as every other Unmarshal row — a Go value of the target type
// from a byte slice — rather than the parse alone, which would be a different
// measurement. It is the code a simdjson-go user has to write to get a typed
// value out; the parity test in simdjson_test.go holds it to encoding/json's
// reading of the fixtures.

// simdjsonPool holds parsed tapes for reuse. Parse takes its previous result
// back to reuse the buffers, which is how the library is meant to be called
// and the analogue of the buffer pools sonic and go-json keep internally. The
// target value is still a fresh zero value on every call.
var simdjsonPool sync.Pool

// SimdjsonUnmarshal decodes data into v, which must be a *TwitterStruct or a
// *Book, by walking simdjson-go's tape; it is exported so bench/shapes can
// measure the same walk on its reference rows.
func SimdjsonUnmarshal(data []byte, v any) error {
	reuse, _ := simdjsonPool.Get().(*simdjson.ParsedJson)
	pj, err := simdjson.Parse(data, reuse)
	if err != nil {
		return err
	}
	defer simdjsonPool.Put(pj)

	it := pj.Iter()
	if t := it.Advance(); t != simdjson.TypeRoot {
		return fmt.Errorf("simdjson: expected a root element, got %v", t)
	}
	var root simdjson.Iter
	t, elem, err := it.Root(&root)
	if err != nil {
		return err
	}
	if t != simdjson.TypeObject {
		return fmt.Errorf("simdjson: expected an object at the root, got %v", t)
	}

	switch x := v.(type) {
	case *TwitterStruct:
		return simdTwitter(elem, x)
	case *Book:
		return simdBook(elem, x)
	default:
		return fmt.Errorf("simdjson: no walker for %T", v)
	}
}

// Scalars. A null leaves the field alone, as encoding/json does.

func simdString(e *simdjson.Iter, t simdjson.Type, dst *string) error {
	if t == simdjson.TypeNull {
		return nil
	}
	s, err := e.String()
	if err != nil {
		return err
	}
	*dst = s
	return nil
}

func simdInt(e *simdjson.Iter, t simdjson.Type, dst *int) error {
	if t == simdjson.TypeNull {
		return nil
	}
	n, err := e.Int()
	if err != nil {
		return err
	}
	*dst = int(n)
	return nil
}

func simdInt64(e *simdjson.Iter, t simdjson.Type, dst *int64) error {
	if t == simdjson.TypeNull {
		return nil
	}
	n, err := e.Int()
	if err != nil {
		return err
	}
	*dst = n
	return nil
}

func simdFloat(e *simdjson.Iter, t simdjson.Type, dst *float64) error {
	if t == simdjson.TypeNull {
		return nil
	}
	f, err := e.Float()
	if err != nil {
		return err
	}
	*dst = f
	return nil
}

func simdBool(e *simdjson.Iter, t simdjson.Type, dst *bool) error {
	if t == simdjson.TypeNull {
		return nil
	}
	b, err := e.Bool()
	if err != nil {
		return err
	}
	*dst = b
	return nil
}

// simdAny is an interface{} field: whatever the document holds there, as
// simdjson-go's own untyped conversion builds it.
func simdAny(e *simdjson.Iter, t simdjson.Type, dst *any) error {
	if t == simdjson.TypeNull {
		return nil
	}
	v, err := e.Interface()
	if err != nil {
		return err
	}
	*dst = v
	return nil
}

// Arrays. Each starts from an empty, non-nil slice, because that is what
// encoding/json leaves behind for `[]`, and a nil slice would encode as null.

func simdInts(e *simdjson.Iter, t simdjson.Type, dst *[]int) error {
	if t == simdjson.TypeNull {
		return nil
	}
	var arr simdjson.Array
	if _, err := e.Array(&arr); err != nil {
		return err
	}
	out := make([]int, 0)
	ai := arr.Iter()
	for {
		if ai.Advance() == simdjson.TypeNone {
			break
		}
		n, err := ai.Int()
		if err != nil {
			return err
		}
		out = append(out, int(n))
	}
	*dst = out
	return nil
}

func simdStrings(e *simdjson.Iter, t simdjson.Type, dst *[]string) error {
	if t == simdjson.TypeNull {
		return nil
	}
	var arr simdjson.Array
	if _, err := e.Array(&arr); err != nil {
		return err
	}
	out := make([]string, 0)
	ai := arr.Iter()
	for {
		if ai.Advance() == simdjson.TypeNone {
			break
		}
		s, err := ai.String()
		if err != nil {
			return err
		}
		out = append(out, s)
	}
	*dst = out
	return nil
}

func simdFloats(e *simdjson.Iter, t simdjson.Type, dst *[]float64) error {
	if t == simdjson.TypeNull {
		return nil
	}
	var arr simdjson.Array
	if _, err := e.Array(&arr); err != nil {
		return err
	}
	out := make([]float64, 0)
	ai := arr.Iter()
	for {
		if ai.Advance() == simdjson.TypeNone {
			break
		}
		f, err := ai.Float()
		if err != nil {
			return err
		}
		out = append(out, f)
	}
	*dst = out
	return nil
}

func simdBools(e *simdjson.Iter, t simdjson.Type, dst *[]bool) error {
	if t == simdjson.TypeNull {
		return nil
	}
	var arr simdjson.Array
	if _, err := e.Array(&arr); err != nil {
		return err
	}
	out := make([]bool, 0)
	ai := arr.Iter()
	for {
		if ai.Advance() == simdjson.TypeNone {
			break
		}
		b, err := ai.Bool()
		if err != nil {
			return err
		}
		out = append(out, b)
	}
	*dst = out
	return nil
}

func simdAnys(e *simdjson.Iter, t simdjson.Type, dst *[]any) error {
	if t == simdjson.TypeNull {
		return nil
	}
	var arr simdjson.Array
	if _, err := e.Array(&arr); err != nil {
		return err
	}
	out := make([]any, 0)
	ai := arr.Iter()
	for {
		if ai.Advance() == simdjson.TypeNone {
			break
		}
		v, err := ai.Interface()
		if err != nil {
			return err
		}
		out = append(out, v)
	}
	*dst = out
	return nil
}

// Objects. Each walks the members in document order and dispatches on the
// name; unknown members are skipped, as encoding/json skips them.

func simdTwitter(e *simdjson.Iter, x *TwitterStruct) error {
	var obj simdjson.Object
	if _, err := e.Object(&obj); err != nil {
		return err
	}
	var m simdjson.Iter
	for {
		name, t, err := obj.NextElementBytes(&m)
		if err != nil {
			return err
		}
		if t == simdjson.TypeNone {
			return nil
		}
		switch string(name) {
		case "statuses":
			if t == simdjson.TypeNull {
				continue
			}
			var arr simdjson.Array
			if _, err := m.Array(&arr); err != nil {
				return err
			}
			out := make([]Statuses, 0)
			ai := arr.Iter()
			for {
				if ai.Advance() == simdjson.TypeNone {
					break
				}
				out = append(out, Statuses{})
				if err := simdStatus(&ai, &out[len(out)-1]); err != nil {
					return err
				}
			}
			x.Statuses = out
		case "search_metadata":
			if t == simdjson.TypeNull {
				continue
			}
			err = simdSearchMetadata(&m, &x.SearchMetadata)
		}
		if err != nil {
			return err
		}
	}
}

func simdSearchMetadata(e *simdjson.Iter, x *SearchMetadata) error {
	var obj simdjson.Object
	if _, err := e.Object(&obj); err != nil {
		return err
	}
	var m simdjson.Iter
	for {
		name, t, err := obj.NextElementBytes(&m)
		if err != nil {
			return err
		}
		if t == simdjson.TypeNone {
			return nil
		}
		switch string(name) {
		case "max_id":
			err = simdInt64(&m, t, &x.MaxID)
		case "since_id":
			err = simdInt64(&m, t, &x.SinceID)
		case "refresh_url":
			err = simdString(&m, t, &x.RefreshURL)
		case "next_results":
			err = simdString(&m, t, &x.NextResults)
		case "count":
			err = simdInt(&m, t, &x.Count)
		case "completed_in":
			err = simdFloat(&m, t, &x.CompletedIn)
		case "since_id_str":
			err = simdString(&m, t, &x.SinceIDStr)
		case "query":
			err = simdString(&m, t, &x.Query)
		case "max_id_str":
			err = simdString(&m, t, &x.MaxIDStr)
		}
		if err != nil {
			return err
		}
	}
}

func simdStatus(e *simdjson.Iter, x *Statuses) error {
	var obj simdjson.Object
	if _, err := e.Object(&obj); err != nil {
		return err
	}
	var m simdjson.Iter
	for {
		name, t, err := obj.NextElementBytes(&m)
		if err != nil {
			return err
		}
		if t == simdjson.TypeNone {
			return nil
		}
		switch string(name) {
		case "coordinates":
			err = simdAny(&m, t, &x.Coordinates)
		case "favorited":
			err = simdBool(&m, t, &x.Favorited)
		case "truncated":
			err = simdBool(&m, t, &x.Truncated)
		case "created_at":
			err = simdString(&m, t, &x.CreatedAt)
		case "id_str":
			err = simdString(&m, t, &x.IDStr)
		case "entities":
			if t == simdjson.TypeNull {
				continue
			}
			err = simdEntities(&m, &x.Entities)
		case "in_reply_to_user_id_str":
			err = simdAny(&m, t, &x.InReplyToUserIDStr)
		case "contributors":
			err = simdAny(&m, t, &x.Contributors)
		case "text":
			err = simdString(&m, t, &x.Text)
		case "metadata":
			if t == simdjson.TypeNull {
				continue
			}
			err = simdMetadata(&m, &x.Metadata)
		case "retweet_count":
			err = simdInt(&m, t, &x.RetweetCount)
		case "in_reply_to_status_id_str":
			err = simdAny(&m, t, &x.InReplyToStatusIDStr)
		case "id":
			err = simdInt64(&m, t, &x.ID)
		case "geo":
			err = simdAny(&m, t, &x.Geo)
		case "retweeted":
			err = simdBool(&m, t, &x.Retweeted)
		case "in_reply_to_user_id":
			err = simdAny(&m, t, &x.InReplyToUserID)
		case "place":
			err = simdAny(&m, t, &x.Place)
		case "user":
			if t == simdjson.TypeNull {
				continue
			}
			err = simdUser(&m, &x.User)
		case "in_reply_to_screen_name":
			err = simdAny(&m, t, &x.InReplyToScreenName)
		case "source":
			err = simdString(&m, t, &x.Source)
		case "in_reply_to_status_id":
			err = simdAny(&m, t, &x.InReplyToStatusID)
		}
		if err != nil {
			return err
		}
	}
}

func simdEntities(e *simdjson.Iter, x *Entities) error {
	var obj simdjson.Object
	if _, err := e.Object(&obj); err != nil {
		return err
	}
	var m simdjson.Iter
	for {
		name, t, err := obj.NextElementBytes(&m)
		if err != nil {
			return err
		}
		if t == simdjson.TypeNone {
			return nil
		}
		switch string(name) {
		case "urls":
			err = simdAnys(&m, t, &x.Urls)
		case "hashtags":
			if t == simdjson.TypeNull {
				continue
			}
			var arr simdjson.Array
			if _, err := m.Array(&arr); err != nil {
				return err
			}
			out := make([]Hashtags, 0)
			ai := arr.Iter()
			for {
				if ai.Advance() == simdjson.TypeNone {
					break
				}
				out = append(out, Hashtags{})
				if err := simdHashtag(&ai, &out[len(out)-1]); err != nil {
					return err
				}
			}
			x.Hashtags = out
		case "user_mentions":
			err = simdAnys(&m, t, &x.UserMentions)
		}
		if err != nil {
			return err
		}
	}
}

func simdHashtag(e *simdjson.Iter, x *Hashtags) error {
	var obj simdjson.Object
	if _, err := e.Object(&obj); err != nil {
		return err
	}
	var m simdjson.Iter
	for {
		name, t, err := obj.NextElementBytes(&m)
		if err != nil {
			return err
		}
		if t == simdjson.TypeNone {
			return nil
		}
		switch string(name) {
		case "text":
			err = simdString(&m, t, &x.Text)
		case "indices":
			err = simdInts(&m, t, &x.Indices)
		}
		if err != nil {
			return err
		}
	}
}

func simdMetadata(e *simdjson.Iter, x *Metadata) error {
	var obj simdjson.Object
	if _, err := e.Object(&obj); err != nil {
		return err
	}
	var m simdjson.Iter
	for {
		name, t, err := obj.NextElementBytes(&m)
		if err != nil {
			return err
		}
		if t == simdjson.TypeNone {
			return nil
		}
		switch string(name) {
		case "iso_language_code":
			err = simdString(&m, t, &x.IsoLanguageCode)
		case "result_type":
			err = simdString(&m, t, &x.ResultType)
		}
		if err != nil {
			return err
		}
	}
}

func simdUser(e *simdjson.Iter, x *User) error {
	var obj simdjson.Object
	if _, err := e.Object(&obj); err != nil {
		return err
	}
	var m simdjson.Iter
	for {
		name, t, err := obj.NextElementBytes(&m)
		if err != nil {
			return err
		}
		if t == simdjson.TypeNone {
			return nil
		}
		switch string(name) {
		case "profile_sidebar_fill_color":
			err = simdString(&m, t, &x.ProfileSidebarFillColor)
		case "profile_sidebar_border_color":
			err = simdString(&m, t, &x.ProfileSidebarBorderColor)
		case "profile_background_tile":
			err = simdBool(&m, t, &x.ProfileBackgroundTile)
		case "name":
			err = simdString(&m, t, &x.Name)
		case "profile_image_url":
			err = simdString(&m, t, &x.ProfileImageURL)
		case "created_at":
			err = simdString(&m, t, &x.CreatedAt)
		case "location":
			err = simdString(&m, t, &x.Location)
		case "follow_request_sent":
			err = simdAny(&m, t, &x.FollowRequestSent)
		case "profile_link_color":
			err = simdString(&m, t, &x.ProfileLinkColor)
		case "is_translator":
			err = simdBool(&m, t, &x.IsTranslator)
		case "id_str":
			err = simdString(&m, t, &x.IDStr)
		case "entities":
			if t == simdjson.TypeNull {
				continue
			}
			err = simdUserEntities(&m, &x.Entities)
		case "default_profile":
			err = simdBool(&m, t, &x.DefaultProfile)
		case "contributors_enabled":
			err = simdBool(&m, t, &x.ContributorsEnabled)
		case "favourites_count":
			err = simdInt(&m, t, &x.FavouritesCount)
		case "url":
			err = simdAny(&m, t, &x.URL)
		case "profile_image_url_https":
			err = simdString(&m, t, &x.ProfileImageURLHTTPS)
		case "utc_offset":
			err = simdInt(&m, t, &x.UtcOffset)
		case "id":
			err = simdInt(&m, t, &x.ID)
		case "profile_use_background_image":
			err = simdBool(&m, t, &x.ProfileUseBackgroundImage)
		case "listed_count":
			err = simdInt(&m, t, &x.ListedCount)
		case "profile_text_color":
			err = simdString(&m, t, &x.ProfileTextColor)
		case "lang":
			err = simdString(&m, t, &x.Lang)
		case "followers_count":
			err = simdInt(&m, t, &x.FollowersCount)
		case "protected":
			err = simdBool(&m, t, &x.Protected)
		case "notifications":
			err = simdAny(&m, t, &x.Notifications)
		case "profile_background_image_url_https":
			err = simdString(&m, t, &x.ProfileBackgroundImageURLHTTPS)
		case "profile_background_color":
			err = simdString(&m, t, &x.ProfileBackgroundColor)
		case "verified":
			err = simdBool(&m, t, &x.Verified)
		case "geo_enabled":
			err = simdBool(&m, t, &x.GeoEnabled)
		case "time_zone":
			err = simdString(&m, t, &x.TimeZone)
		case "description":
			err = simdString(&m, t, &x.Description)
		case "default_profile_image":
			err = simdBool(&m, t, &x.DefaultProfileImage)
		case "profile_background_image_url":
			err = simdString(&m, t, &x.ProfileBackgroundImageURL)
		case "statuses_count":
			err = simdInt(&m, t, &x.StatusesCount)
		case "friends_count":
			err = simdInt(&m, t, &x.FriendsCount)
		case "following":
			err = simdAny(&m, t, &x.Following)
		case "show_all_inline_media":
			err = simdBool(&m, t, &x.ShowAllInlineMedia)
		case "screen_name":
			err = simdString(&m, t, &x.ScreenName)
		}
		if err != nil {
			return err
		}
	}
}

func simdUserEntities(e *simdjson.Iter, x *UserEntities) error {
	var obj simdjson.Object
	if _, err := e.Object(&obj); err != nil {
		return err
	}
	var m simdjson.Iter
	for {
		name, t, err := obj.NextElementBytes(&m)
		if err != nil {
			return err
		}
		if t == simdjson.TypeNone {
			return nil
		}
		switch string(name) {
		case "url":
			if t == simdjson.TypeNull {
				continue
			}
			err = simdURL(&m, &x.URL)
		case "description":
			if t == simdjson.TypeNull {
				continue
			}
			err = simdDescription(&m, &x.Description)
		}
		if err != nil {
			return err
		}
	}
}

func simdURL(e *simdjson.Iter, x *URL) error {
	var obj simdjson.Object
	if _, err := e.Object(&obj); err != nil {
		return err
	}
	var m simdjson.Iter
	for {
		name, t, err := obj.NextElementBytes(&m)
		if err != nil {
			return err
		}
		if t == simdjson.TypeNone {
			return nil
		}
		if string(name) != "urls" || t == simdjson.TypeNull {
			continue
		}
		var arr simdjson.Array
		if _, err := m.Array(&arr); err != nil {
			return err
		}
		out := make([]Urls, 0)
		ai := arr.Iter()
		for {
			if ai.Advance() == simdjson.TypeNone {
				break
			}
			out = append(out, Urls{})
			if err := simdUrls(&ai, &out[len(out)-1]); err != nil {
				return err
			}
		}
		x.Urls = out
	}
}

func simdUrls(e *simdjson.Iter, x *Urls) error {
	var obj simdjson.Object
	if _, err := e.Object(&obj); err != nil {
		return err
	}
	var m simdjson.Iter
	for {
		name, t, err := obj.NextElementBytes(&m)
		if err != nil {
			return err
		}
		if t == simdjson.TypeNone {
			return nil
		}
		switch string(name) {
		case "expanded_url":
			err = simdAny(&m, t, &x.ExpandedURL)
		case "url":
			err = simdString(&m, t, &x.URL)
		case "indices":
			err = simdInts(&m, t, &x.Indices)
		}
		if err != nil {
			return err
		}
	}
}

func simdDescription(e *simdjson.Iter, x *Description) error {
	var obj simdjson.Object
	if _, err := e.Object(&obj); err != nil {
		return err
	}
	var m simdjson.Iter
	for {
		name, t, err := obj.NextElementBytes(&m)
		if err != nil {
			return err
		}
		if t == simdjson.TypeNone {
			return nil
		}
		if string(name) == "urls" {
			if err := simdAnys(&m, t, &x.Urls); err != nil {
				return err
			}
		}
	}
}

func simdBook(e *simdjson.Iter, x *Book) error {
	var obj simdjson.Object
	if _, err := e.Object(&obj); err != nil {
		return err
	}
	var m simdjson.Iter
	for {
		name, t, err := obj.NextElementBytes(&m)
		if err != nil {
			return err
		}
		if t == simdjson.TypeNone {
			return nil
		}
		switch string(name) {
		case "id":
			err = simdInt(&m, t, &x.BookId)
		case "ids":
			err = simdInts(&m, t, &x.BookIds)
		case "title":
			err = simdString(&m, t, &x.Title)
		case "titles":
			err = simdStrings(&m, t, &x.Titles)
		case "price":
			err = simdFloat(&m, t, &x.Price)
		case "prices":
			err = simdFloats(&m, t, &x.Prices)
		case "hot":
			err = simdBool(&m, t, &x.Hot)
		case "hots":
			err = simdBools(&m, t, &x.Hots)
		case "author":
			if t == simdjson.TypeNull {
				continue
			}
			err = simdAuthor(&m, &x.Author)
		case "authors":
			if t == simdjson.TypeNull {
				continue
			}
			var arr simdjson.Array
			if _, err := m.Array(&arr); err != nil {
				return err
			}
			out := make([]Author, 0)
			ai := arr.Iter()
			for {
				if ai.Advance() == simdjson.TypeNone {
					break
				}
				out = append(out, Author{})
				if err := simdAuthor(&ai, &out[len(out)-1]); err != nil {
					return err
				}
			}
			x.Authors = out
		case "weights":
			err = simdInts(&m, t, &x.Weights)
		}
		if err != nil {
			return err
		}
	}
}

func simdAuthor(e *simdjson.Iter, x *Author) error {
	var obj simdjson.Object
	if _, err := e.Object(&obj); err != nil {
		return err
	}
	var m simdjson.Iter
	for {
		name, t, err := obj.NextElementBytes(&m)
		if err != nil {
			return err
		}
		if t == simdjson.TypeNone {
			return nil
		}
		switch string(name) {
		case "name":
			err = simdString(&m, t, &x.Name)
		case "age":
			err = simdInt(&m, t, &x.Age)
		case "male":
			err = simdBool(&m, t, &x.Male)
		}
		if err != nil {
			return err
		}
	}
}
