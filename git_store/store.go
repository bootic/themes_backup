package git_store

type Store struct {
}

func (s *Store) Write(fileName string) error {
	return nil
}

func NewStore(dir string) *Store {
	return &Store{}
}
