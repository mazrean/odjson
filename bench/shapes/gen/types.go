package gen

import "time"

// Item is a mid-size record with the member mix of a typical API resource:
// an integer id, a few strings that are unique per record, a timestamp, a
// float, a bool, a small string slice, a small map and an optional nested
// object.
type Item struct {
	ID        int64             `json:"id"`
	UUID      string            `json:"uuid"`
	Name      string            `json:"name"`
	Email     string            `json:"email"`
	CreatedAt time.Time         `json:"created_at"`
	Score     float64           `json:"score"`
	Count     int               `json:"count"`
	Active    bool              `json:"active"`
	Tags      []string          `json:"tags"`
	Attrs     map[string]string `json:"attrs"`
	Owner     *Owner            `json:"owner,omitempty"`
}

// Owner is the nested object inside an Item.
type Owner struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// Page is a list of items behind a top-level object, the shape of a
// paginated API response. The same items behind a top-level array are the
// []Item shape.
type Page struct {
	Items      []Item `json:"items"`
	Total      int    `json:"total"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// Text is a slice of strings. The benchmark varies the content of the
// strings, which is what decides which branch of the UTF-8 and escape scans
// runs.
type Text struct {
	Lines []string `json:"lines"`
}

// IDs is a slice of strings that never repeat, so the decoder's string cache
// never hits.
type IDs struct {
	Values []string `json:"values"`
}

// Generic holds a document as any, so every byte goes through the runtime's
// generic parser rather than through generated code.
type Generic struct {
	Payload any `json:"payload"`
}

// Numbers covers the numeric spellings the fixtures do not have: full
// precision floats, exponents, float32, and the ends of the integer ranges.
type Numbers struct {
	I64 []int64   `json:"i64"`
	U64 []uint64  `json:"u64"`
	I16 []int16   `json:"i16"`
	F32 []float32 `json:"f32"`
	F64 []float64 `json:"f64"`
	Sci []float64 `json:"sci"`
}

// DenseDoc is a list of Dense rows.
type DenseDoc struct {
	Rows []Dense `json:"rows"`
}

// Dense has many short scalar members: a few bytes of value per member, which
// makes member names and structural characters most of the document.
type Dense struct {
	F00 int32   `json:"f00"`
	F01 int32   `json:"f01"`
	F02 int32   `json:"f02"`
	F03 int32   `json:"f03"`
	F04 int32   `json:"f04"`
	F05 int32   `json:"f05"`
	F06 int32   `json:"f06"`
	F07 int32   `json:"f07"`
	F08 int32   `json:"f08"`
	F09 int32   `json:"f09"`
	F10 int32   `json:"f10"`
	F11 int32   `json:"f11"`
	F12 int32   `json:"f12"`
	F13 int32   `json:"f13"`
	F14 int32   `json:"f14"`
	F15 int32   `json:"f15"`
	F16 int32   `json:"f16"`
	F17 int32   `json:"f17"`
	F18 int32   `json:"f18"`
	F19 int32   `json:"f19"`
	F20 int32   `json:"f20"`
	F21 int32   `json:"f21"`
	F22 int32   `json:"f22"`
	F23 int32   `json:"f23"`
	F24 int32   `json:"f24"`
	F25 int32   `json:"f25"`
	F26 int32   `json:"f26"`
	F27 int32   `json:"f27"`
	F28 int32   `json:"f28"`
	F29 int32   `json:"f29"`
	F30 int32   `json:"f30"`
	F31 int32   `json:"f31"`
	B00 bool    `json:"b00"`
	B01 bool    `json:"b01"`
	B02 bool    `json:"b02"`
	B03 bool    `json:"b03"`
	B04 bool    `json:"b04"`
	B05 bool    `json:"b05"`
	B06 bool    `json:"b06"`
	B07 bool    `json:"b07"`
	B08 bool    `json:"b08"`
	B09 bool    `json:"b09"`
	B10 bool    `json:"b10"`
	B11 bool    `json:"b11"`
	B12 bool    `json:"b12"`
	B13 bool    `json:"b13"`
	B14 bool    `json:"b14"`
	B15 bool    `json:"b15"`
	X00 float64 `json:"x00"`
	X01 float64 `json:"x01"`
	X02 float64 `json:"x02"`
	X03 float64 `json:"x03"`
	X04 float64 `json:"x04"`
	X05 float64 `json:"x05"`
	X06 float64 `json:"x06"`
	X07 float64 `json:"x07"`
}

// SparseDoc is a list of Sparse rows.
type SparseDoc struct {
	Rows []Sparse `json:"rows"`
}

// Sparse has many optional members of which a document sets a few, some of
// them to null: the shape of a wide record type read from a store that omits
// what is unset.
type Sparse struct {
	F00 *int64  `json:"f00,omitempty"`
	F01 *int64  `json:"f01,omitempty"`
	F02 *int64  `json:"f02,omitempty"`
	F03 *int64  `json:"f03,omitempty"`
	F04 *int64  `json:"f04,omitempty"`
	F05 *int64  `json:"f05,omitempty"`
	F06 *int64  `json:"f06,omitempty"`
	F07 *int64  `json:"f07,omitempty"`
	F08 *int64  `json:"f08,omitempty"`
	F09 *int64  `json:"f09,omitempty"`
	F10 *int64  `json:"f10,omitempty"`
	F11 *int64  `json:"f11,omitempty"`
	F12 *int64  `json:"f12,omitempty"`
	F13 *int64  `json:"f13,omitempty"`
	F14 *int64  `json:"f14,omitempty"`
	F15 *int64  `json:"f15,omitempty"`
	S00 *string `json:"s00,omitempty"`
	S01 *string `json:"s01,omitempty"`
	S02 *string `json:"s02,omitempty"`
	S03 *string `json:"s03,omitempty"`
	S04 *string `json:"s04,omitempty"`
	S05 *string `json:"s05,omitempty"`
	S06 *string `json:"s06,omitempty"`
	S07 *string `json:"s07,omitempty"`
	S08 *string `json:"s08,omitempty"`
	S09 *string `json:"s09,omitempty"`
	S10 *string `json:"s10,omitempty"`
	S11 *string `json:"s11,omitempty"`
	S12 *string `json:"s12,omitempty"`
	S13 *string `json:"s13,omitempty"`
	S14 *string `json:"s14,omitempty"`
	S15 *string `json:"s15,omitempty"`
	N   *Owner  `json:"n,omitempty"`
}

// Canada is nativejson-benchmark's canada.json: a GeoJSON FeatureCollection
// whose bytes are almost entirely full precision floats.
type Canada struct {
	Type     string    `json:"type"`
	Features []Feature `json:"features"`
}

// Feature is one GeoJSON feature.
type Feature struct {
	Type       string            `json:"type"`
	Properties map[string]string `json:"properties"`
	Geometry   Geometry          `json:"geometry"`
}

// Geometry is a GeoJSON polygon.
type Geometry struct {
	Type        string        `json:"type"`
	Coordinates [][][]float64 `json:"coordinates"`
}

// CitmCatalog is nativejson-benchmark's citm_catalog.json: maps keyed by
// numeric strings, many integers, optional strings that are mostly null, and
// French text.
type CitmCatalog struct {
	AreaNames                map[string]string  `json:"areaNames"`
	AudienceSubCategoryNames map[string]string  `json:"audienceSubCategoryNames"`
	BlockNames               map[string]string  `json:"blockNames"`
	Events                   map[string]Event   `json:"events"`
	Performances             []Performance      `json:"performances"`
	SeatCategoryNames        map[string]string  `json:"seatCategoryNames"`
	SubTopicNames            map[string]string  `json:"subTopicNames"`
	SubjectNames             map[string]string  `json:"subjectNames"`
	TopicNames               map[string]string  `json:"topicNames"`
	TopicSubTopics           map[string][]int64 `json:"topicSubTopics"`
	VenueNames               map[string]string  `json:"venueNames"`
}

// Event is one entry of CitmCatalog.Events.
type Event struct {
	Description *string `json:"description"`
	ID          int64   `json:"id"`
	Logo        *string `json:"logo"`
	Name        string  `json:"name"`
	SubTopicIds []int64 `json:"subTopicIds"`
	SubjectCode *string `json:"subjectCode"`
	Subtitle    *string `json:"subtitle"`
	TopicIds    []int64 `json:"topicIds"`
}

// Performance is one entry of CitmCatalog.Performances.
type Performance struct {
	EventID        int64          `json:"eventId"`
	ID             int64          `json:"id"`
	Logo           *string        `json:"logo"`
	Name           *string        `json:"name"`
	Prices         []Price        `json:"prices"`
	SeatCategories []SeatCategory `json:"seatCategories"`
	SeatMapImage   *string        `json:"seatMapImage"`
	Start          int64          `json:"start"`
	VenueCode      string         `json:"venueCode"`
}

// Price is one entry of Performance.Prices.
type Price struct {
	Amount                int64 `json:"amount"`
	AudienceSubCategoryID int64 `json:"audienceSubCategoryId"`
	SeatCategoryID        int64 `json:"seatCategoryId"`
}

// SeatCategory is one entry of Performance.SeatCategories.
type SeatCategory struct {
	Areas          []Area `json:"areas"`
	SeatCategoryID int64  `json:"seatCategoryId"`
}

// Area is one entry of SeatCategory.Areas.
type Area struct {
	AreaID   int64   `json:"areaId"`
	BlockIds []int64 `json:"blockIds"`
}
