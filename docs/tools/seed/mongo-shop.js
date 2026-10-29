db.dropDatabase();
db.customers.insertMany([
  { _id: 1, email: 'ann.lee@example.com', name: 'Ann Lee', country: 'US', tags: ['vip'], address: { city: 'Austin', zip: '73301' }, signedUp: new Date('2023-02-11') },
  { _id: 2, email: 'bob.novak@example.com', name: 'Bob Novak', country: 'GB', tags: [], address: { city: 'London', zip: 'N1 9GU' }, signedUp: new Date('2023-05-02') },
  { _id: 3, email: 'chen.okafor@example.com', name: 'Chen Okafor', country: 'DE', tags: ['newsletter'], address: { city: 'Berlin', zip: '10115' }, signedUp: new Date('2024-01-20') },
  { _id: 4, email: 'dana.petrov@example.com', name: 'Dana Petrov', country: 'FR', tags: ['vip', 'newsletter'], address: { city: 'Lyon', zip: '69001' }, signedUp: new Date('2024-03-08') },
]);
const products = [];
const names = ['Studio headphones', 'Bluetooth speaker', 'USB microphone', 'Mirrorless camera body', '35mm f/1.8 lens', 'Chef knife 20cm', 'Cast iron pan 28cm', 'Espresso grinder', 'Running shoes', 'Yoga mat', 'Mechanical keyboard', '27 inch monitor'];
names.forEach((n, i) => products.push({ _id: i + 1, sku: 'P-' + String(i + 1).padStart(4, '0'), name: n, price: NumberDecimal((20 + i * 37.5).toFixed(2)), stock: (i * 13) % 50, attrs: { color: ['black', 'teal', 'silver'][i % 3], weightKg: +(0.2 + i * 0.3).toFixed(1) }, active: i % 5 !== 4 }));
db.products.insertMany(products);
const orders = [];
for (let i = 1; i <= 150; i++) orders.push({ customerId: 1 + (i % 4), status: ['new', 'paid', 'shipped', 'delivered', 'cancelled'][i % 5], placedAt: new Date(Date.UTC(2024, 0, 1, 9) + i * 5 * 3600 * 1000), items: [{ productId: 1 + (i % 12), qty: 1 + (i % 3) }, ...(i % 2 ? [{ productId: 1 + ((i * 5) % 12), qty: 1 }] : [])], total: NumberDecimal((50 + (i * 37) % 900).toFixed(2)) });
db.orders.insertMany(orders);
db.orders.createIndex({ customerId: 1, placedAt: -1 });
db.products.createIndex({ sku: 1 }, { unique: true });
db.createCollection('audit_log', { capped: true, size: 1048576 });
db.audit_log.insertMany([{ what: 'seeded', at: new Date() }]);
