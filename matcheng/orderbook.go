package matcheng

import (
	"errors"
	"fmt"

	bt "github.com/google/btree"
)

// import ""

const (
	BUYSIDE  = 1
	SELLSIDE = 2
)

// PRECISION is the last effective decimal digit of the price of currency pair
const PRECISION = 0.00000001

type OrderPart struct {
	id   string
	time uint64
	qty  float64
}

type PriceLevel struct {
	Price  float64
	orders []OrderPart
}

type PriceLevelInterface interface {
	addOrder(id string, time uint64, qty float64) (int, error)
	removeOrder(id string) (OrderPart, int, error)
	Less(than bt.Item) bool
}

func compareBuy(p1 float64, p2 float64) int {
	d := (p2 - p1)
	switch {
	case d >= PRECISION:
		return -1
	case d <= -PRECISION:
		return 1
	default:
		return 0
	}
}

func compareSell(p1 float64, p2 float64) int {
	return -compareBuy(p1, p2)
}

func newPriceLevel(price float64, orders []OrderPart) *PriceLevel {
	return &PriceLevel{price, orders}
}

//addOrder would implicitly called with sequence of 'time' parameter
func (l *PriceLevel) addOrder(id string, time uint64, qty float64) (int, error) {
	// TODO: need benchmark - queue is not expected to be very long (less than hundreds)
	for _, o := range l.orders {
		if o.id == id {
			return 0, fmt.Errorf("Order %s has existed in the price level.", id)
		}
	}
	l.orders = append(l.orders, OrderPart{id, time, qty})
	return len(l.orders), nil

}

func (l *PriceLevel) removeOrder(id string) (OrderPart, int, error) {
	for i, o := range l.orders {
		if o.id == id {
			l.orders = append(l.orders[:i], l.orders[i+1])
			return o, len(l.orders), nil
		}
	}
	// not found
	return OrderPart{}, len(l.orders), fmt.Errorf("order %s doesn't exist.", id)
}

type OverLappedLevel struct {
	Price      float64
	BuyOrders  []OrderPart
	SellOrders []OrderPart
	SellTotal  float64
	BuyTotal   float64
	Executions float64
	Surplus    float64
}

// OrderBookInterface is a generic sequenced order to quickly get the spread to match.
// It can be implemented in different structures but here a fast unrolled-linked list,
// or/and google/B-Tree are chosen, still need performance benchmark to justify this.
type OrderBookInterface interface {
	GetOverlappedRange(overlapped []OverLappedLevel) int
	InsertOrder(id string, side int, time uint, price float64, qty float64) (*PriceLevel, error)
	RemoveOrder(id string, side int, price float64) (OrderPart, error)
}

type OrderBookOnULList struct {
	buyQueue  *ULList
	sellQueue *ULList
}

type OrderBookOnBTree struct {
	buyQueue  *bt.BTree
	sellQueue *bt.BTree
}

func (ob *OrderBookOnULList) getSideQueue(side int) *ULList {
	switch side {
	case BUYSIDE:
		return ob.buyQueue
	case SELLSIDE:
		return ob.sellQueue
	}
	return nil
}

func NewOrderBookOnULList(d int) *OrderBookOnULList {
	//TODO: find out the best degree
	// 16 is my magic number, hopefully the real overlapped levels are less
	return &OrderBookOnULList{NewULList(4096, 16, compareBuy),
		NewULList(4096, 16, compareSell)}
}

func (ob *OrderBookOnULList) GetOverlappedRange(overlapped []OverLappedLevel) int {
	return 0
}

func (ob *OrderBookOnULList) InsertOrder(id string, side int, time uint64, price float64, qty float64) (*PriceLevel, error) {
	q := ob.getSideQueue(side)
	var pl *PriceLevel
	if pl = q.GetPriceLevel(price); pl == nil {
		// price level not exist, insert a new one
		pl = newPriceLevel(price, []OrderPart{{id, time, qty}})
	} else {
		if _, err := pl.addOrder(id, time, qty); err != nil {
			return pl, err
		}
	}
	if !q.SetPriceLevel(pl) {
		return pl, fmt.Errorf("Failed to insert order %s at price %f", id, price)
	}
	return pl, nil
}

func (ob *OrderBookOnULList) RemoveOrder(id string, side int, price float64) (OrderPart, error) {
	q := ob.getSideQueue(side)
	var pl *PriceLevel
	if pl := q.GetPriceLevel(price); pl == nil {
		return OrderPart{}, fmt.Errorf("order price %f doesn't exist at side %d.", price, side)
	}
	op, total, ok := pl.removeOrder(id)
	if ok != nil {
		return op, ok
	}
	//price level is gone
	if total == 0.0 {
		q.DeletePriceLevel(pl.Price)
	}
	return op, ok
}

type BuyPriceLevel struct {
	PriceLevel
}

type SellPriceLevel struct {
	PriceLevel
}

func (l BuyPriceLevel) Less(than bt.Item) bool {
	return (than.(BuyPriceLevel).Price - l.Price) >= PRECISION
}

func (l SellPriceLevel) Less(than bt.Item) bool {
	return (l.Price - than.(SellPriceLevel).Price) >= PRECISION
}

/*
func (l *BuyPriceLevel) addOrder(id string, time uint64, qty float64) (float64, error) {
	return l.Price.addOrder(id, time, qty)
} */

func newPriceLevelBySide(price float64, orders []OrderPart, side int) PriceLevelInterface {
	switch side {
	case BUYSIDE:
		return &BuyPriceLevel{PriceLevel{price, orders}}
	case SELLSIDE:
		return &SellPriceLevel{PriceLevel{price, orders}}
	}
	return &BuyPriceLevel{PriceLevel{price, orders}}
}

func newPriceLevelKey(price float64, side int) PriceLevelInterface {
	switch side {
	case BUYSIDE:
		return &BuyPriceLevel{PriceLevel{Price: price}}
	case SELLSIDE:
		return &SellPriceLevel{PriceLevel{Price: price}}
	}
	return &BuyPriceLevel{PriceLevel{Price: price}}
}

func NewOrderBookOnBTree(d int) *OrderBookOnBTree {
	//TODO: find out the best degree
	// 16 is my magic number, hopefully the real overlapped levels are less
	return &OrderBookOnBTree{bt.New(8), bt.New(8)}
}

func (ob *OrderBookOnBTree) getSideQueue(side int) *bt.BTree {
	switch side {
	case BUYSIDE:
		return ob.buyQueue
	case SELLSIDE:
		return ob.sellQueue
	}
	return nil
}

func (ob *OrderBookOnBTree) GetOverlappedRange(overlapped []OverLappedLevel) int {
	return 0
}

func toPriceLevel(pi PriceLevelInterface, side int) *PriceLevel {
	switch side {
	case BUYSIDE:
		if pl, ok := pi.(*BuyPriceLevel); ok {
			return &pl.PriceLevel
		}
	case SELLSIDE:
		if pl, ok := pi.(*SellPriceLevel); ok {
			return &pl.PriceLevel
		}
	}
	return nil
}

func (ob *OrderBookOnBTree) InsertOrder(id string, side int, time uint64, price float64, qty float64) (*PriceLevel, error) {
	q := ob.getSideQueue(side)
	var pl PriceLevelInterface
	if pl := q.Get(newPriceLevelKey(price, side)); pl == nil {
		// price level not exist, insert a new one
		pl = newPriceLevelBySide(price, []OrderPart{{id, time, qty}}, side)
	} else {
		if pl2, ok := pl.(PriceLevelInterface); !ok {
			return nil, errors.New("Severe error: Wrong type item inserted into OrderBook")
		} else {
			if _, e := pl2.addOrder(id, time, qty); e != nil {
				return toPriceLevel(pl2, side), e
			}
		}
	}
	if q.ReplaceOrInsert(pl) == nil {
		return toPriceLevel(pl, side), fmt.Errorf("Failed to insert order %s at price %f", id, price)
	}
	return toPriceLevel(pl, side), nil
}

func (ob *OrderBookOnBTree) RemoveOrder(id string, side int, price float64) (OrderPart, error) {
	q := ob.getSideQueue(side)
	var pl PriceLevelInterface
	if pl := q.Get(newPriceLevelKey(price, side)); pl == nil {
		return OrderPart{}, fmt.Errorf("order price %f doesn't exist at side %d.", price, side)
	}
	if pl2, ok := pl.(PriceLevelInterface); !ok {
		return OrderPart{}, errors.New("Severe error: Wrong type item inserted into OrderBook")
	} else {
		op, total, err := pl2.removeOrder(id)
		if err != nil {
			return op, err
		}
		//price level is gone
		if total == 0 {
			q.Delete(pl)
		}
		return op, err
	}
}