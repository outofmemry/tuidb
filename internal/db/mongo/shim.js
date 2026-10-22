// Mongo-shell shim evaluated by goja. Natives: __op, __run, __oid, __print, __db.
// Values cross into Go as extended JSON, so BSON-only types are plain {"$..."} objects.
(function (g) {
  function ser(v, ind) {
    return JSON.stringify(v, function (k, x) {
      var raw = this[k];
      if (raw instanceof Date) return isNaN(raw.getTime()) ? null : { "$date": raw.toISOString() };
      if (raw instanceof RegExp) return { "$regularExpression": { pattern: raw.source, options: raw.flags.replace(/[^imsx]/g, "") } };
      return x;
    }, ind);
  }
  function hasOwn(o, p) { return Object.prototype.hasOwnProperty.call(o, p); }

  g.ObjectId = function (h) { return { "$oid": h === undefined ? __oid() : String(h) }; };
  g.ISODate = function (s) { return s === undefined ? new Date() : new Date(s); };
  g.NumberLong = function (n) { return { "$numberLong": String(n) }; };
  g.NumberInt = function (n) { return { "$numberInt": String(Math.trunc(Number(n))) }; };
  g.NumberDecimal = function (s) { return { "$numberDecimal": String(s) }; };
  g.UUID = function (s) { return { "$uuid": String(s) }; };

  function call(dbn, coll, op, args) {
    var r = JSON.parse(__op(dbn, coll, op, ser({ a: args })));
    var v = r.v;
    if (r.m && v !== null && typeof v === "object") Object.defineProperty(v, "__msg", { value: r.m });
    return v;
  }
  g.__call = call;

  function Cursor(s) {
    var buf = null, i = 0, self = this;
    Object.defineProperty(this, "_s", { value: s });
    function all() { if (!buf) buf = JSON.parse(__run(ser(s))).v; return buf; }
    var m = {
      sort: function (x) { s.sort = x; return self; },
      limit: function (n) { s.limit = Number(n); return self; },
      skip: function (n) { s.skip = Number(n); return self; },
      project: function (p) { s.proj = p; return self; },
      projection: function (p) { s.proj = p; return self; },
      pretty: function () { return self; },
      toArray: all,
      forEach: function (f) { all().forEach(f); },
      map: function (f) { return all().map(f); },
      hasNext: function () { return i < all().length; },
      next: function () { return all()[i++]; },
      itcount: function () { return all().length; },
      count: function () { return s.kind === "find" ? call(s.db, s.coll, "countDocuments", [s.a[0]]) : all().length; }
    };
    Object.keys(m).forEach(function (k) { Object.defineProperty(self, k, { value: m[k] }); });
  }

  var ops = ["findOne", "countDocuments", "estimatedDocumentCount", "distinct", "insertOne", "insertMany", "updateOne",
    "updateMany", "replaceOne", "deleteOne", "deleteMany", "createIndex", "dropIndex", "getIndexes", "drop", "renameCollection"];

  function mkColl(dbn, name) {
    var m = {
      getName: function () { return name; },
      find: function (f, p) { return new Cursor({ db: dbn, coll: name, kind: "find", a: [f || {}, p || null], limit: 0, skip: 0 }); },
      aggregate: function (p, o) { return new Cursor({ db: dbn, coll: name, kind: "aggregate", a: [p || [], o || {}], limit: 0, skip: 0 }); }
    };
    ops.forEach(function (op) {
      m[op] = function () { return call(dbn, name, op, Array.prototype.slice.call(arguments)); };
    });
    m.count = m.countDocuments;
    m.remove = m.deleteMany;
    m.insert = function (d) { return Array.isArray(d) ? m.insertMany(d) : m.insertOne(d); };
    return new Proxy(m, {
      get: function (t, p) {
        if (typeof p !== "string") return undefined;
        return hasOwn(t, p) ? t[p] : mkColl(dbn, name + "." + p);
      }
    });
  }

  function mkDB(dbn) {
    var m = {
      getName: function () { return dbn; },
      getCollection: function (n) { return mkColl(dbn, String(n)); },
      getSiblingDB: function (n) { return mkDB(String(n)); },
      getCollectionNames: function () { return call(dbn, "", "listCollectionNames", []); },
      createCollection: function (n, o) { return call(dbn, String(n), "createCollection", [o]); },
      createView: function (n, src, p, o) { return call(dbn, String(n), "createView", [src, p, o]); },
      dropDatabase: function () { return call(dbn, "", "dropDatabase", []); },
      runCommand: function (c) { return call(dbn, "", "runCommand", [c]); },
      adminCommand: function (c) { return call("admin", "", "runCommand", [c]); }
    };
    return new Proxy(m, {
      get: function (t, p) {
        if (typeof p !== "string") return undefined;
        return hasOwn(t, p) ? t[p] : mkColl(dbn, p);
      }
    });
  }
  g.db = mkDB(__db);

  g.print = function () {
    __print(Array.prototype.map.call(arguments, function (x) { return typeof x === "string" ? x : ser(x); }).join(" "));
  };
  g.printjson = function (x) { __print(ser(x, 2)); };
  g.__named = function (arr, col) { return arr.map(function (x) { var o = {}; o[col] = x; return o; }); };

  g.__finish = function (v) {
    if (v === undefined) return '{"k":"u"}';
    if (v instanceof Cursor) return ser({ k: "c", s: v._s });
    return ser({ k: "v", v: v, m: v !== null && typeof v === "object" ? v.__msg : undefined });
  };
})(this);
