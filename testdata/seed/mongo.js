const d = db.getSiblingDB('webtuidb');
d.customers.insertMany([
  {_id: 1, name: 'Ann Lee', email: 'ann@example.com', notes: 'vip', balance: 120.5, tags: ['a','b'], address: {city: 'Oslo', zip: '0150'}, created_at: new Date('2024-01-02T03:04:05Z')},
  {_id: 2, name: "Bob O'Neil", email: 'bob@example.com', notes: null, balance: 0},
  {_id: 3, name: 'Cy 100%_done', balance: 5, extra: {nested: [1, 2, {deep: true}]}}
]);
d.orders.insertMany([{customer_id: 1, total: 10, status: 'paid'}, {customer_id: 1, total: 25.5, status: 'new'}, {customer_id: 2, total: 99.99, status: 'paid'}]);
d.customers.createIndex({email: 1}, {unique: true, sparse: true, name: 'email_1'});
d.createView('paid_orders', 'orders', [{$match: {status: 'paid'}}]);
