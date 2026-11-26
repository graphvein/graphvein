package com.example.app;

import com.example.app.model.Item;             // JV-I2 (resolution is unaffected)
import java.util.List;                         // JV-I8
import static org.junit.Assert.*;              // JV-I8 (static on-demand)
import java . util /* c */ . Map;              // JV-I8 (whitespace and comments removed)

class App {
    List<Item> items;
    Map<String, Item> byName;
}
