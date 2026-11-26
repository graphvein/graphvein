import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

class ItemsWidget extends StatelessWidget {
  const ItemsWidget({super.key});

  Future<String> loadItems() async {
    final response = await http.get(Uri.parse('https://example.invalid/api/items'));
    return response.body;
  }

  @override
  Widget build(BuildContext context) {
    return FutureBuilder<String>(
      future: loadItems(),
      builder: (context, snapshot) => Text(snapshot.data ?? 'Loading'),
    );
  }
}

void main() {
  runApp(const MaterialApp(home: ItemsWidget()));
}
