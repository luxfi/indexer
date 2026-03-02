package explorer

import (
	"log/slog"

	"github.com/hanzoai/base/core"
)

// ensureCollections creates user-facing collections in Base's own DB.
// Chain data (blocks, txs, tokens) is read directly from the indexer's SQLite.
// These collections handle user accounts, API keys, watchlists, and tags.
func (p *plugin) ensureCollections() error {
	collections := []func() (*core.Collection, error){
		p.watchlistCollection,
		p.addressTagsCollection,
		p.txTagsCollection,
		p.customABIsCollection,
	}

	for _, fn := range collections {
		col, err := fn()
		if err != nil {
			return err
		}
		if col == nil {
			continue
		}
		p.app.Logger().Info("creating explorer collection", slog.String("name", col.Name))
		if err := p.app.Save(col); err != nil {
			return err
		}
	}
	return nil
}

func collectionExists(app core.App, name string) bool {
	_, err := app.FindCollectionByNameOrId(name)
	return err == nil
}

func (p *plugin) watchlistCollection() (*core.Collection, error) {
	if collectionExists(p.app, "explorer_watchlist") {
		return nil, nil
	}
	c := core.NewBaseCollection("explorer_watchlist")
	c.Fields.Add(&core.TextField{Name: "address", Required: true, Max: 42})
	c.Fields.Add(&core.TextField{Name: "name", Max: 255})
	c.Fields.Add(&core.BoolField{Name: "notify_incoming"})
	c.Fields.Add(&core.BoolField{Name: "notify_outgoing"})
	c.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
	c.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})

	// Only the owner can access their watchlist
	authRule := "@request.auth.id != ''"
	c.ListRule = &authRule
	c.ViewRule = &authRule
	c.CreateRule = &authRule
	c.UpdateRule = &authRule
	c.DeleteRule = &authRule

	c.AddIndex("idx_watchlist_address", false, "address", "")
	return c, nil
}

func (p *plugin) addressTagsCollection() (*core.Collection, error) {
	if collectionExists(p.app, "explorer_address_tags") {
		return nil, nil
	}
	c := core.NewBaseCollection("explorer_address_tags")
	c.Fields.Add(&core.TextField{Name: "address", Required: true, Max: 42})
	c.Fields.Add(&core.TextField{Name: "tag", Required: true, Max: 100})
	c.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})

	authRule := "@request.auth.id != ''"
	c.ListRule = &authRule
	c.ViewRule = &authRule
	c.CreateRule = &authRule
	c.DeleteRule = &authRule

	c.AddIndex("idx_addr_tags_address", false, "address", "")
	return c, nil
}

func (p *plugin) txTagsCollection() (*core.Collection, error) {
	if collectionExists(p.app, "explorer_tx_tags") {
		return nil, nil
	}
	c := core.NewBaseCollection("explorer_tx_tags")
	c.Fields.Add(&core.TextField{Name: "tx_hash", Required: true, Max: 66})
	c.Fields.Add(&core.TextField{Name: "tag", Required: true, Max: 100})
	c.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})

	authRule := "@request.auth.id != ''"
	c.ListRule = &authRule
	c.ViewRule = &authRule
	c.CreateRule = &authRule
	c.DeleteRule = &authRule

	c.AddIndex("idx_tx_tags_hash", false, "tx_hash", "")
	return c, nil
}

func (p *plugin) customABIsCollection() (*core.Collection, error) {
	if collectionExists(p.app, "explorer_custom_abis") {
		return nil, nil
	}
	c := core.NewBaseCollection("explorer_custom_abis")
	c.Fields.Add(&core.TextField{Name: "address", Required: true, Max: 42})
	c.Fields.Add(&core.TextField{Name: "name", Required: true, Max: 255})
	c.Fields.Add(&core.JSONField{Name: "abi", Required: true})
	c.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})

	authRule := "@request.auth.id != ''"
	c.ListRule = &authRule
	c.ViewRule = &authRule
	c.CreateRule = &authRule
	c.UpdateRule = &authRule
	c.DeleteRule = &authRule

	c.AddIndex("idx_custom_abi_address", false, "address", "")
	return c, nil
}
