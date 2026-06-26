package com.acme.shop.catalog;

import com.external.orm.AbstractRepository;

public class CatalogRepository extends AbstractRepository {
    public String lookup(String sku) {
        return sku;
    }
}
