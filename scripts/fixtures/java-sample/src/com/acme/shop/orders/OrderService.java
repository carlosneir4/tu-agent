package com.acme.shop.orders;

public class OrderService extends BaseService {
    @Override
    public String describe() {
        return "order";
    }

    public int total(int a, int b) {
        return a + b;
    }
}
