import { useEffect, useState } from "react";

export function App() {
  const [items, setItems] = useState([]);
  useEffect(() => {
    fetch("/api/items").then((response) => response.json()).then(setItems);
  }, []);
  return <ul>{items.map((item) => <li key={item.id}>{item.name}</li>)}</ul>;
}
