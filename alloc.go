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

func makeMachFreeList(n int) *machFreeList { return &machFreeList{make([]*Mach, 0, n)} }
func makePageFreeList(n int) *pageFreeList { return &pageFreeList{make([]*page, 0, n)} }

type machFreeList struct{ f []*Mach }
type pageFreeList struct{ f []*page }

func (mfl *machFreeList) FreeMach(m *Mach)  { mfl.f = append(mfl.f, m) }
func (pfl *pageFreeList) FreePage(pg *page) { pfl.f = append(pfl.f, pg) }

func (mfl *machFreeList) AllocMach() (*Mach, error) {
	if i := len(mfl.f) - 1; i >= 0 {
		m := mfl.f[i]
		mfl.f = mfl.f[:i]
		return m, nil
	}
	return &Mach{}, nil
}

func (pfl *pageFreeList) AllocPage() *page {
	if i := len(pfl.f) - 1; i >= 0 {
		pg := pfl.f[i]
		pfl.f = pfl.f[:i]
		for i := range pg.d {
			pg.d[i] = 0
		}
		return pg
	}
	return &page{}
}
