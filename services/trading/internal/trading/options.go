package trading

import (
	"context"
	"errors"
	"sort"
	"time"

	tradingpb "github.com/RAF-SI-2025/Banka-3-Backend/pkg/proto/trading"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// requireActuary denies clients and returns the caller identity on success.
// Options are actuary-only per spec p.59 — at this layer that translates to
// "must be an employee" (the portal gates agent/supervisor features further).
func (s *Server) requireActuary(ctx context.Context) error {
	caller, err := s.ResolveCaller(ctx)
	if err != nil {
		return err
	}
	if caller.IsClient {
		return status.Error(codes.PermissionDenied, "options are available to employees only")
	}
	return nil
}

// stockListingPrice returns the current listing price for the given stock.
// Used both to anchor the options grid and to return shared_price.
func (s *Server) stockListingPrice(stockID int64) (int64, error) {
	var l Listing
	err := s.db_gorm.Where("stock_id = ?", stockID).First(&l).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, status.Error(codes.NotFound, "stock has no listing")
		}
		return 0, status.Errorf(codes.Internal, "%v", err)
	}
	return l.Price, nil
}

// ListOptionDates returns the distinct settlement dates for a stock's options
// along with days-to-expiry, computed from today (UTC). Empty result is fine
// — the caller renders an empty dropdown.
func (s *Server) ListOptionDates(ctx context.Context, req *tradingpb.ListOptionDatesRequest) (*tradingpb.ListOptionDatesResponse, error) {
	if err := s.requireActuary(ctx); err != nil {
		return nil, err
	}
	if req.StockId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "stock_id required")
	}

	var dates []time.Time
	err := s.db_gorm.Model(&Option{}).
		Where("stock_id = ?", req.StockId).
		Distinct("settlement_date").
		Order("settlement_date ASC").
		Pluck("settlement_date", &dates).Error
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	out := make([]*tradingpb.OptionDate, 0, len(dates))
	for _, d := range dates {
		days := int32(d.UTC().Truncate(24*time.Hour).Sub(today) / (24 * time.Hour))
		out = append(out, &tradingpb.OptionDate{
			SettlementDateUnix: d.Unix(),
			DaysToExpiry:       days,
		})
	}
	return &tradingpb.ListOptionDatesResponse{Dates: out}, nil
}

// ListOptions returns the CALLS/PUTS grid for the chosen expiry, centred on
// the current stock price with N strikes above and below (Approach 1).
func (s *Server) ListOptions(ctx context.Context, req *tradingpb.ListOptionsRequest) (*tradingpb.ListOptionsResponse, error) {
	if err := s.requireActuary(ctx); err != nil {
		return nil, err
	}
	if req.StockId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "stock_id required")
	}
	if req.Settlement == "" {
		return nil, status.Error(codes.InvalidArgument, "settlement required (YYYY-MM-DD)")
	}
	settle, err := time.Parse("2006-01-02", req.Settlement)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "settlement must be YYYY-MM-DD: %v", err)
	}

	price, err := s.stockListingPrice(req.StockId)
	if err != nil {
		return nil, err
	}

	var opts []Option
	if err := s.db_gorm.
		Where("stock_id = ? AND settlement_date = ?", req.StockId, settle).
		Order("strike_price ASC").
		Find(&opts).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}

	// Group by strike, splitting into call/put.
	type pair struct {
		call *Option
		put  *Option
	}
	byStrike := map[int64]*pair{}
	var strikes []int64
	for i := range opts {
		o := &opts[i]
		p, ok := byStrike[o.StrikePrice]
		if !ok {
			p = &pair{}
			byStrike[o.StrikePrice] = p
			strikes = append(strikes, o.StrikePrice)
		}
		switch o.OptionType {
		case OptionCall:
			p.call = o
		case OptionPut:
			p.put = o
		}
	}
	sort.Slice(strikes, func(i, j int) bool { return strikes[i] < strikes[j] })

	// Select N strikes above and N below the spot. A strike exactly at spot
	// counts as "at the money" and goes into the "below-or-equal" bucket so
	// it always shows up when it exists.
	selected := selectCentredStrikes(strikes, price, int(req.Strikes))

	rows := make([]*tradingpb.OptionGridRow, 0, len(selected))
	for _, k := range selected {
		p := byStrike[k]
		rows = append(rows, &tradingpb.OptionGridRow{
			Strike: k,
			Call:   optionToProto(p.call),
			Put:    optionToProto(p.put),
		})
	}

	return &tradingpb.ListOptionsResponse{
		StockId:     req.StockId,
		Settlement:  req.Settlement,
		SharedPrice: price,
		Rows:        rows,
	}, nil
}

// selectCentredStrikes picks up to n strikes at-or-below spot plus up to n
// strikes strictly above spot, preserving ascending order. n == 0 means
// "return everything" — convenient for tests and for callers that want the
// full chain.
func selectCentredStrikes(strikes []int64, spot int64, n int) []int64 {
	if n <= 0 {
		return strikes
	}
	split := sort.Search(len(strikes), func(i int) bool { return strikes[i] > spot })
	lo := split - n
	if lo < 0 {
		lo = 0
	}
	hi := split + n
	if hi > len(strikes) {
		hi = len(strikes)
	}
	return strikes[lo:hi]
}

func optionToProto(o *Option) *tradingpb.OptionContract {
	if o == nil {
		return nil
	}
	return &tradingpb.OptionContract{
		Id:                 o.ID,
		Ticker:             o.Ticker,
		Side:               string(o.OptionType),
		Strike:             o.StrikePrice,
		Last:               o.Premium,
		Theta:              0,
		Bid:                0,
		Ask:                0,
		Volume:             0,
		OpenInterest:       o.OpenInterest,
		SettlementDateUnix: o.SettlementDate.Unix(),
	}
}
