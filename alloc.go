package stackvm

import "sync"

type machAllocator interface {
	AllocMach() (*Mach, error)
	FreeMach(*Mach)
}

type pageAllocator interface {
	AllocPage() *page
	FreePage(*page)
}

var (
	machPool = sync.Pool{New: func() interface{} { return &Mach{} }}
	pagePool sync.Pool
)

type _machPoolAllocator struct{}

func (mpa _machPoolAllocator) AllocMach() (*Mach, error) {
	return machPool.Get().(*Mach), nil
}

func (mpa _machPoolAllocator) FreeMach(m *Mach) {
	machPool.Put(m)
}

type _pagePoolAllocator struct{}

func (ppa _pagePoolAllocator) AllocPage() *page {
	if v := pagePool.Get(); v != nil {
		if pg, ok := v.(*page); ok {
			for i := range pg.d {
				pg.d[i] = 0
			}
			return pg
		}
	}
	return &page{r: 0}
}

func (ppa _pagePoolAllocator) FreePage(pg *page) {
	pagePool.Put(pg)
}

var (
	machPoolAllocator machAllocator = _machPoolAllocator{}
	pagePoolAllocator pageAllocator = _pagePoolAllocator{}
)
