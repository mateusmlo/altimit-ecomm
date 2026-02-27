INSERT INTO products (product_id, name, description, price, stock) VALUES
	('550e8400-e29b-41d4-a716-446655440001', 'Laptop', 'High-performance laptop', 999.99, 10),
	('550e8400-e29b-41d4-a716-446655440002', 'Mouse', 'Wireless mouse', 29.99, 50),
	('550e8400-e29b-41d4-a716-446655440003', 'Keyboard', 'Mechanical keyboard', 79.99, 30)
ON CONFLICT (product_id) DO NOTHING;
