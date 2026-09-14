package gojay

import (
	"github.com/francoispqt/gojay"
)

// Hand-written gojay marshalers and unmarshalers for the vendored types. gojay's
// generator rejects the interface{} fields these types carry, so this file is
// what a gojay user writes for them: one MarshalJSONObject / IsNil /
// UnmarshalJSONObject / NKeys quartet per struct, and a MarshalerJSONArray /
// UnmarshalerJSONArray pair per slice of structs.
//
// The semantics follow encoding/json where gojay leaves a choice, because that
// is what TestParity compares against: a nil interface{} is written as null
// (gojay's AddInterfaceKey silently drops it), a nil slice as null and an
// empty one as [], and decoding an array starts from an empty slice so that
// [] does not come back as nil. NKeys returns 0 everywhere: a non-zero count
// makes gojay stop after that many members, known or not, and none of these
// documents promise to carry exactly the struct's members and no others.

// addAnyKey writes an interface{} member. gojay's own AddInterfaceKey covers
// the scalars; objects, arrays and nil need the wrappers below.
func addAnyKey(enc *gojay.Encoder, key string, v any) {
	switch x := v.(type) {
	case nil:
		enc.AddNullKey(key)
	case map[string]any:
		enc.AddObjectKey(key, anyObject(x))
	case []any:
		if x == nil {
			enc.AddNullKey(key)
			return
		}
		enc.AddArrayKey(key, anySlice(x))
	default:
		enc.AddInterfaceKey(key, v)
	}
}

// addAny writes an interface{} array element.
func addAny(enc *gojay.Encoder, v any) {
	switch x := v.(type) {
	case nil:
		enc.AddNull()
	case map[string]any:
		enc.AddObject(anyObject(x))
	case []any:
		if x == nil {
			enc.AddNull()
			return
		}
		enc.AddArray(anySlice(x))
	default:
		enc.AddInterface(v)
	}
}

// decodeAny reads an interface{} member. gojay hands the value to
// encoding/json underneath, which is the library's own choice.
func decodeAny(dec *gojay.Decoder, dst *any) error {
	return dec.Interface(dst)
}

// decodeAnys reads a []interface{} member: nil for null, an empty slice for
// [], the way encoding/json fills the field.
func decodeAnys(dec *gojay.Decoder, dst *[]any) error {
	var v any
	if err := dec.Interface(&v); err != nil {
		return err
	}
	*dst, _ = v.([]any)
	return nil
}

type anyObject map[string]any

func (o anyObject) MarshalJSONObject(enc *gojay.Encoder) {
	for k, v := range o {
		addAnyKey(enc, k, v)
	}
}

func (o anyObject) IsNil() bool { return o == nil }

type anySlice []any

func (s anySlice) MarshalJSONArray(enc *gojay.Encoder) {
	for _, v := range s {
		addAny(enc, v)
	}
}

func (s anySlice) IsNil() bool { return s == nil }

// addArrayKey writes a slice member as null when nil and as an array
// otherwise, which is encoding/json's distinction; gojay's ArrayKey writes []
// for both.
func addArrayKey(enc *gojay.Encoder, key string, v gojay.MarshalerJSONArray) {
	if v.IsNil() {
		enc.AddNullKey(key)
		return
	}
	enc.AddArrayKey(key, v)
}

// Slices of scalars.

type intSlice []int

func (s *intSlice) UnmarshalJSONArray(dec *gojay.Decoder) error {
	var v int
	if err := dec.Int(&v); err != nil {
		return err
	}
	*s = append(*s, v)
	return nil
}

func (s intSlice) MarshalJSONArray(enc *gojay.Encoder) {
	for _, v := range s {
		enc.AddInt(v)
	}
}

func (s intSlice) IsNil() bool { return s == nil }

func decodeInts(dec *gojay.Decoder, dst *[]int) error {
	s := intSlice(make([]int, 0))
	if err := dec.Array(&s); err != nil {
		return err
	}
	*dst = s
	return nil
}

type stringSlice []string

func (s *stringSlice) UnmarshalJSONArray(dec *gojay.Decoder) error {
	var v string
	if err := dec.String(&v); err != nil {
		return err
	}
	*s = append(*s, v)
	return nil
}

func (s stringSlice) MarshalJSONArray(enc *gojay.Encoder) {
	for _, v := range s {
		enc.AddString(v)
	}
}

func (s stringSlice) IsNil() bool { return s == nil }

func decodeStrings(dec *gojay.Decoder, dst *[]string) error {
	s := stringSlice(make([]string, 0))
	if err := dec.Array(&s); err != nil {
		return err
	}
	*dst = s
	return nil
}

type floatSlice []float64

func (s *floatSlice) UnmarshalJSONArray(dec *gojay.Decoder) error {
	var v float64
	if err := dec.Float64(&v); err != nil {
		return err
	}
	*s = append(*s, v)
	return nil
}

func (s floatSlice) MarshalJSONArray(enc *gojay.Encoder) {
	for _, v := range s {
		enc.AddFloat64(v)
	}
}

func (s floatSlice) IsNil() bool { return s == nil }

func decodeFloats(dec *gojay.Decoder, dst *[]float64) error {
	s := floatSlice(make([]float64, 0))
	if err := dec.Array(&s); err != nil {
		return err
	}
	*dst = s
	return nil
}

type boolSlice []bool

func (s *boolSlice) UnmarshalJSONArray(dec *gojay.Decoder) error {
	var v bool
	if err := dec.Bool(&v); err != nil {
		return err
	}
	*s = append(*s, v)
	return nil
}

func (s boolSlice) MarshalJSONArray(enc *gojay.Encoder) {
	for _, v := range s {
		enc.AddBool(v)
	}
}

func (s boolSlice) IsNil() bool { return s == nil }

func decodeBools(dec *gojay.Decoder, dst *[]bool) error {
	s := boolSlice(make([]bool, 0))
	if err := dec.Array(&s); err != nil {
		return err
	}
	*dst = s
	return nil
}

// TwitterStruct

func (x *TwitterStruct) MarshalJSONObject(enc *gojay.Encoder) {
	addArrayKey(enc, "statuses", statusesSlice(x.Statuses))
	enc.AddObjectKey("search_metadata", &x.SearchMetadata)
}

func (x *TwitterStruct) IsNil() bool { return x == nil }

func (x *TwitterStruct) UnmarshalJSONObject(dec *gojay.Decoder, key string) error {
	switch key {
	case "statuses":
		s := statusesSlice(make([]Statuses, 0))
		if err := dec.Array(&s); err != nil {
			return err
		}
		x.Statuses = s
	case "search_metadata":
		return dec.Object(&x.SearchMetadata)
	}
	return nil
}

func (x *TwitterStruct) NKeys() int { return 0 }

type statusesSlice []Statuses

func (s *statusesSlice) UnmarshalJSONArray(dec *gojay.Decoder) error {
	*s = append(*s, Statuses{})
	return dec.Object(&(*s)[len(*s)-1])
}

func (s statusesSlice) MarshalJSONArray(enc *gojay.Encoder) {
	for i := range s {
		enc.AddObject(&s[i])
	}
}

func (s statusesSlice) IsNil() bool { return s == nil }

// Statuses

func (x *Statuses) MarshalJSONObject(enc *gojay.Encoder) {
	addAnyKey(enc, "coordinates", x.Coordinates)
	enc.AddBoolKey("favorited", x.Favorited)
	enc.AddBoolKey("truncated", x.Truncated)
	enc.AddStringKey("created_at", x.CreatedAt)
	enc.AddStringKey("id_str", x.IDStr)
	enc.AddObjectKey("entities", &x.Entities)
	addAnyKey(enc, "in_reply_to_user_id_str", x.InReplyToUserIDStr)
	addAnyKey(enc, "contributors", x.Contributors)
	enc.AddStringKey("text", x.Text)
	enc.AddObjectKey("metadata", &x.Metadata)
	enc.AddIntKey("retweet_count", x.RetweetCount)
	addAnyKey(enc, "in_reply_to_status_id_str", x.InReplyToStatusIDStr)
	enc.AddInt64Key("id", x.ID)
	addAnyKey(enc, "geo", x.Geo)
	enc.AddBoolKey("retweeted", x.Retweeted)
	addAnyKey(enc, "in_reply_to_user_id", x.InReplyToUserID)
	addAnyKey(enc, "place", x.Place)
	enc.AddObjectKey("user", &x.User)
	addAnyKey(enc, "in_reply_to_screen_name", x.InReplyToScreenName)
	enc.AddStringKey("source", x.Source)
	addAnyKey(enc, "in_reply_to_status_id", x.InReplyToStatusID)
}

func (x *Statuses) IsNil() bool { return x == nil }

func (x *Statuses) UnmarshalJSONObject(dec *gojay.Decoder, key string) error {
	switch key {
	case "coordinates":
		return decodeAny(dec, &x.Coordinates)
	case "favorited":
		return dec.Bool(&x.Favorited)
	case "truncated":
		return dec.Bool(&x.Truncated)
	case "created_at":
		return dec.String(&x.CreatedAt)
	case "id_str":
		return dec.String(&x.IDStr)
	case "entities":
		return dec.Object(&x.Entities)
	case "in_reply_to_user_id_str":
		return decodeAny(dec, &x.InReplyToUserIDStr)
	case "contributors":
		return decodeAny(dec, &x.Contributors)
	case "text":
		return dec.String(&x.Text)
	case "metadata":
		return dec.Object(&x.Metadata)
	case "retweet_count":
		return dec.Int(&x.RetweetCount)
	case "in_reply_to_status_id_str":
		return decodeAny(dec, &x.InReplyToStatusIDStr)
	case "id":
		return dec.Int64(&x.ID)
	case "geo":
		return decodeAny(dec, &x.Geo)
	case "retweeted":
		return dec.Bool(&x.Retweeted)
	case "in_reply_to_user_id":
		return decodeAny(dec, &x.InReplyToUserID)
	case "place":
		return decodeAny(dec, &x.Place)
	case "user":
		return dec.Object(&x.User)
	case "in_reply_to_screen_name":
		return decodeAny(dec, &x.InReplyToScreenName)
	case "source":
		return dec.String(&x.Source)
	case "in_reply_to_status_id":
		return decodeAny(dec, &x.InReplyToStatusID)
	}
	return nil
}

func (x *Statuses) NKeys() int { return 0 }

// SearchMetadata

func (x *SearchMetadata) MarshalJSONObject(enc *gojay.Encoder) {
	enc.AddInt64Key("max_id", x.MaxID)
	enc.AddInt64Key("since_id", x.SinceID)
	enc.AddStringKey("refresh_url", x.RefreshURL)
	enc.AddStringKey("next_results", x.NextResults)
	enc.AddIntKey("count", x.Count)
	enc.AddFloatKey("completed_in", x.CompletedIn)
	enc.AddStringKey("since_id_str", x.SinceIDStr)
	enc.AddStringKey("query", x.Query)
	enc.AddStringKey("max_id_str", x.MaxIDStr)
}

func (x *SearchMetadata) IsNil() bool { return x == nil }

func (x *SearchMetadata) UnmarshalJSONObject(dec *gojay.Decoder, key string) error {
	switch key {
	case "max_id":
		return dec.Int64(&x.MaxID)
	case "since_id":
		return dec.Int64(&x.SinceID)
	case "refresh_url":
		return dec.String(&x.RefreshURL)
	case "next_results":
		return dec.String(&x.NextResults)
	case "count":
		return dec.Int(&x.Count)
	case "completed_in":
		return dec.Float64(&x.CompletedIn)
	case "since_id_str":
		return dec.String(&x.SinceIDStr)
	case "query":
		return dec.String(&x.Query)
	case "max_id_str":
		return dec.String(&x.MaxIDStr)
	}
	return nil
}

func (x *SearchMetadata) NKeys() int { return 0 }

// Entities

func (x *Entities) MarshalJSONObject(enc *gojay.Encoder) {
	addArrayKey(enc, "urls", anySlice(x.Urls))
	addArrayKey(enc, "hashtags", hashtagsSlice(x.Hashtags))
	addArrayKey(enc, "user_mentions", anySlice(x.UserMentions))
}

func (x *Entities) IsNil() bool { return x == nil }

func (x *Entities) UnmarshalJSONObject(dec *gojay.Decoder, key string) error {
	switch key {
	case "urls":
		return decodeAnys(dec, &x.Urls)
	case "hashtags":
		s := hashtagsSlice(make([]Hashtags, 0))
		if err := dec.Array(&s); err != nil {
			return err
		}
		x.Hashtags = s
	case "user_mentions":
		return decodeAnys(dec, &x.UserMentions)
	}
	return nil
}

func (x *Entities) NKeys() int { return 0 }

type hashtagsSlice []Hashtags

func (s *hashtagsSlice) UnmarshalJSONArray(dec *gojay.Decoder) error {
	*s = append(*s, Hashtags{})
	return dec.Object(&(*s)[len(*s)-1])
}

func (s hashtagsSlice) MarshalJSONArray(enc *gojay.Encoder) {
	for i := range s {
		enc.AddObject(&s[i])
	}
}

func (s hashtagsSlice) IsNil() bool { return s == nil }

// Hashtags

func (x *Hashtags) MarshalJSONObject(enc *gojay.Encoder) {
	enc.AddStringKey("text", x.Text)
	addArrayKey(enc, "indices", intSlice(x.Indices))
}

func (x *Hashtags) IsNil() bool { return x == nil }

func (x *Hashtags) UnmarshalJSONObject(dec *gojay.Decoder, key string) error {
	switch key {
	case "text":
		return dec.String(&x.Text)
	case "indices":
		return decodeInts(dec, &x.Indices)
	}
	return nil
}

func (x *Hashtags) NKeys() int { return 0 }

// Metadata

func (x *Metadata) MarshalJSONObject(enc *gojay.Encoder) {
	enc.AddStringKey("iso_language_code", x.IsoLanguageCode)
	enc.AddStringKey("result_type", x.ResultType)
}

func (x *Metadata) IsNil() bool { return x == nil }

func (x *Metadata) UnmarshalJSONObject(dec *gojay.Decoder, key string) error {
	switch key {
	case "iso_language_code":
		return dec.String(&x.IsoLanguageCode)
	case "result_type":
		return dec.String(&x.ResultType)
	}
	return nil
}

func (x *Metadata) NKeys() int { return 0 }

// User

func (x *User) MarshalJSONObject(enc *gojay.Encoder) {
	enc.AddStringKey("profile_sidebar_fill_color", x.ProfileSidebarFillColor)
	enc.AddStringKey("profile_sidebar_border_color", x.ProfileSidebarBorderColor)
	enc.AddBoolKey("profile_background_tile", x.ProfileBackgroundTile)
	enc.AddStringKey("name", x.Name)
	enc.AddStringKey("profile_image_url", x.ProfileImageURL)
	enc.AddStringKey("created_at", x.CreatedAt)
	enc.AddStringKey("location", x.Location)
	addAnyKey(enc, "follow_request_sent", x.FollowRequestSent)
	enc.AddStringKey("profile_link_color", x.ProfileLinkColor)
	enc.AddBoolKey("is_translator", x.IsTranslator)
	enc.AddStringKey("id_str", x.IDStr)
	enc.AddObjectKey("entities", &x.Entities)
	enc.AddBoolKey("default_profile", x.DefaultProfile)
	enc.AddBoolKey("contributors_enabled", x.ContributorsEnabled)
	enc.AddIntKey("favourites_count", x.FavouritesCount)
	addAnyKey(enc, "url", x.URL)
	enc.AddStringKey("profile_image_url_https", x.ProfileImageURLHTTPS)
	enc.AddIntKey("utc_offset", x.UtcOffset)
	enc.AddIntKey("id", x.ID)
	enc.AddBoolKey("profile_use_background_image", x.ProfileUseBackgroundImage)
	enc.AddIntKey("listed_count", x.ListedCount)
	enc.AddStringKey("profile_text_color", x.ProfileTextColor)
	enc.AddStringKey("lang", x.Lang)
	enc.AddIntKey("followers_count", x.FollowersCount)
	enc.AddBoolKey("protected", x.Protected)
	addAnyKey(enc, "notifications", x.Notifications)
	enc.AddStringKey("profile_background_image_url_https", x.ProfileBackgroundImageURLHTTPS)
	enc.AddStringKey("profile_background_color", x.ProfileBackgroundColor)
	enc.AddBoolKey("verified", x.Verified)
	enc.AddBoolKey("geo_enabled", x.GeoEnabled)
	enc.AddStringKey("time_zone", x.TimeZone)
	enc.AddStringKey("description", x.Description)
	enc.AddBoolKey("default_profile_image", x.DefaultProfileImage)
	enc.AddStringKey("profile_background_image_url", x.ProfileBackgroundImageURL)
	enc.AddIntKey("statuses_count", x.StatusesCount)
	enc.AddIntKey("friends_count", x.FriendsCount)
	addAnyKey(enc, "following", x.Following)
	enc.AddBoolKey("show_all_inline_media", x.ShowAllInlineMedia)
	enc.AddStringKey("screen_name", x.ScreenName)
}

func (x *User) IsNil() bool { return x == nil }

func (x *User) UnmarshalJSONObject(dec *gojay.Decoder, key string) error {
	switch key {
	case "profile_sidebar_fill_color":
		return dec.String(&x.ProfileSidebarFillColor)
	case "profile_sidebar_border_color":
		return dec.String(&x.ProfileSidebarBorderColor)
	case "profile_background_tile":
		return dec.Bool(&x.ProfileBackgroundTile)
	case "name":
		return dec.String(&x.Name)
	case "profile_image_url":
		return dec.String(&x.ProfileImageURL)
	case "created_at":
		return dec.String(&x.CreatedAt)
	case "location":
		return dec.String(&x.Location)
	case "follow_request_sent":
		return decodeAny(dec, &x.FollowRequestSent)
	case "profile_link_color":
		return dec.String(&x.ProfileLinkColor)
	case "is_translator":
		return dec.Bool(&x.IsTranslator)
	case "id_str":
		return dec.String(&x.IDStr)
	case "entities":
		return dec.Object(&x.Entities)
	case "default_profile":
		return dec.Bool(&x.DefaultProfile)
	case "contributors_enabled":
		return dec.Bool(&x.ContributorsEnabled)
	case "favourites_count":
		return dec.Int(&x.FavouritesCount)
	case "url":
		return decodeAny(dec, &x.URL)
	case "profile_image_url_https":
		return dec.String(&x.ProfileImageURLHTTPS)
	case "utc_offset":
		return dec.Int(&x.UtcOffset)
	case "id":
		return dec.Int(&x.ID)
	case "profile_use_background_image":
		return dec.Bool(&x.ProfileUseBackgroundImage)
	case "listed_count":
		return dec.Int(&x.ListedCount)
	case "profile_text_color":
		return dec.String(&x.ProfileTextColor)
	case "lang":
		return dec.String(&x.Lang)
	case "followers_count":
		return dec.Int(&x.FollowersCount)
	case "protected":
		return dec.Bool(&x.Protected)
	case "notifications":
		return decodeAny(dec, &x.Notifications)
	case "profile_background_image_url_https":
		return dec.String(&x.ProfileBackgroundImageURLHTTPS)
	case "profile_background_color":
		return dec.String(&x.ProfileBackgroundColor)
	case "verified":
		return dec.Bool(&x.Verified)
	case "geo_enabled":
		return dec.Bool(&x.GeoEnabled)
	case "time_zone":
		return dec.String(&x.TimeZone)
	case "description":
		return dec.String(&x.Description)
	case "default_profile_image":
		return dec.Bool(&x.DefaultProfileImage)
	case "profile_background_image_url":
		return dec.String(&x.ProfileBackgroundImageURL)
	case "statuses_count":
		return dec.Int(&x.StatusesCount)
	case "friends_count":
		return dec.Int(&x.FriendsCount)
	case "following":
		return decodeAny(dec, &x.Following)
	case "show_all_inline_media":
		return dec.Bool(&x.ShowAllInlineMedia)
	case "screen_name":
		return dec.String(&x.ScreenName)
	}
	return nil
}

func (x *User) NKeys() int { return 0 }

// UserEntities

func (x *UserEntities) MarshalJSONObject(enc *gojay.Encoder) {
	enc.AddObjectKey("url", &x.URL)
	enc.AddObjectKey("description", &x.Description)
}

func (x *UserEntities) IsNil() bool { return x == nil }

func (x *UserEntities) UnmarshalJSONObject(dec *gojay.Decoder, key string) error {
	switch key {
	case "url":
		return dec.Object(&x.URL)
	case "description":
		return dec.Object(&x.Description)
	}
	return nil
}

func (x *UserEntities) NKeys() int { return 0 }

// URL

func (x *URL) MarshalJSONObject(enc *gojay.Encoder) {
	addArrayKey(enc, "urls", urlsSlice(x.Urls))
}

func (x *URL) IsNil() bool { return x == nil }

func (x *URL) UnmarshalJSONObject(dec *gojay.Decoder, key string) error {
	if key == "urls" {
		s := urlsSlice(make([]Urls, 0))
		if err := dec.Array(&s); err != nil {
			return err
		}
		x.Urls = s
	}
	return nil
}

func (x *URL) NKeys() int { return 0 }

type urlsSlice []Urls

func (s *urlsSlice) UnmarshalJSONArray(dec *gojay.Decoder) error {
	*s = append(*s, Urls{})
	return dec.Object(&(*s)[len(*s)-1])
}

func (s urlsSlice) MarshalJSONArray(enc *gojay.Encoder) {
	for i := range s {
		enc.AddObject(&s[i])
	}
}

func (s urlsSlice) IsNil() bool { return s == nil }

// Urls

func (x *Urls) MarshalJSONObject(enc *gojay.Encoder) {
	addAnyKey(enc, "expanded_url", x.ExpandedURL)
	enc.AddStringKey("url", x.URL)
	addArrayKey(enc, "indices", intSlice(x.Indices))
}

func (x *Urls) IsNil() bool { return x == nil }

func (x *Urls) UnmarshalJSONObject(dec *gojay.Decoder, key string) error {
	switch key {
	case "expanded_url":
		return decodeAny(dec, &x.ExpandedURL)
	case "url":
		return dec.String(&x.URL)
	case "indices":
		return decodeInts(dec, &x.Indices)
	}
	return nil
}

func (x *Urls) NKeys() int { return 0 }

// Description

func (x *Description) MarshalJSONObject(enc *gojay.Encoder) {
	addArrayKey(enc, "urls", anySlice(x.Urls))
}

func (x *Description) IsNil() bool { return x == nil }

func (x *Description) UnmarshalJSONObject(dec *gojay.Decoder, key string) error {
	if key == "urls" {
		return decodeAnys(dec, &x.Urls)
	}
	return nil
}

func (x *Description) NKeys() int { return 0 }

// Book

func (x *Book) MarshalJSONObject(enc *gojay.Encoder) {
	enc.AddIntKey("id", x.BookId)
	addArrayKey(enc, "ids", intSlice(x.BookIds))
	enc.AddStringKey("title", x.Title)
	addArrayKey(enc, "titles", stringSlice(x.Titles))
	enc.AddFloatKey("price", x.Price)
	addArrayKey(enc, "prices", floatSlice(x.Prices))
	enc.AddBoolKey("hot", x.Hot)
	addArrayKey(enc, "hots", boolSlice(x.Hots))
	enc.AddObjectKey("author", &x.Author)
	addArrayKey(enc, "authors", authorsSlice(x.Authors))
	addArrayKey(enc, "weights", intSlice(x.Weights))
}

func (x *Book) IsNil() bool { return x == nil }

func (x *Book) UnmarshalJSONObject(dec *gojay.Decoder, key string) error {
	switch key {
	case "id":
		return dec.Int(&x.BookId)
	case "ids":
		return decodeInts(dec, &x.BookIds)
	case "title":
		return dec.String(&x.Title)
	case "titles":
		return decodeStrings(dec, &x.Titles)
	case "price":
		return dec.Float64(&x.Price)
	case "prices":
		return decodeFloats(dec, &x.Prices)
	case "hot":
		return dec.Bool(&x.Hot)
	case "hots":
		return decodeBools(dec, &x.Hots)
	case "author":
		return dec.Object(&x.Author)
	case "authors":
		s := authorsSlice(make([]Author, 0))
		if err := dec.Array(&s); err != nil {
			return err
		}
		x.Authors = s
	case "weights":
		return decodeInts(dec, &x.Weights)
	}
	return nil
}

func (x *Book) NKeys() int { return 0 }

type authorsSlice []Author

func (s *authorsSlice) UnmarshalJSONArray(dec *gojay.Decoder) error {
	*s = append(*s, Author{})
	return dec.Object(&(*s)[len(*s)-1])
}

func (s authorsSlice) MarshalJSONArray(enc *gojay.Encoder) {
	for i := range s {
		enc.AddObject(&s[i])
	}
}

func (s authorsSlice) IsNil() bool { return s == nil }

// Author

func (x *Author) MarshalJSONObject(enc *gojay.Encoder) {
	enc.AddStringKey("name", x.Name)
	enc.AddIntKey("age", x.Age)
	enc.AddBoolKey("male", x.Male)
}

func (x *Author) IsNil() bool { return x == nil }

func (x *Author) UnmarshalJSONObject(dec *gojay.Decoder, key string) error {
	switch key {
	case "name":
		return dec.String(&x.Name)
	case "age":
		return dec.Int(&x.Age)
	case "male":
		return dec.Bool(&x.Male)
	}
	return nil
}

func (x *Author) NKeys() int { return 0 }
