// Oracle for the Axum 0.8 route shape fixture: prints the serde_json
// serialization of sample values of the fixture's response types, then
// serves the fixture app on 127.0.0.1 until killed.
use route_shape_axum::models::*;

fn main() {
    let samples = serde_json::json!({
        "Item/note_none": serde_json::to_value(Item { id: 1, name: "a".into(), note: None, tags: vec!["t".into()], kind: Kind::LongPlay, secret: "s".into() }).unwrap(),
        "Item/note_some": serde_json::to_value(Item { id: 1, name: "a".into(), note: Some("n".into()), tags: vec![], kind: Kind::Cassette, secret: "s".into() }).unwrap(),
        "Kind": [serde_json::to_value(Kind::Book).unwrap(), serde_json::to_value(Kind::LongPlay).unwrap(), serde_json::to_value(Kind::Cassette).unwrap()],
        "Health": serde_json::to_value(Health { up_since: 3, ok: true }).unwrap(),
        "TagResult": serde_json::to_value(TagResult { id: 2, label: "l".into() }).unwrap(),
        "Envelope<Kind>": serde_json::to_value(Envelope { data: Kind::Book, count: 1 }).unwrap(),
        "Envelope<Vec<Item>>": serde_json::to_value(Envelope::<Vec<Item>> { data: vec![], count: 0 }).unwrap(),
    });
    println!("{}", samples);
    let port: u16 = std::env::args().nth(1).and_then(|p| p.parse().ok()).unwrap_or(0);
    let rt = tokio::runtime::Runtime::new().unwrap();
    rt.block_on(async move {
        let listener = tokio::net::TcpListener::bind(("127.0.0.1", port)).await.unwrap();
        eprintln!("listening {}", listener.local_addr().unwrap());
        axum::serve(listener, route_shape_axum::app()).await.unwrap();
    });
}
