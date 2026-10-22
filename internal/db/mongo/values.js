// BSON value helpers, shared by the shell (shim.js) and the restricted raw-filter runtime, which
// gets nothing else: no db, no natives. __ser renders a value as extended JSON.
(function (g) {
  g.__ser = function (v, ind) {
    return JSON.stringify(v, function (k, x) {
      var raw = this[k];
      if (raw instanceof Date) return isNaN(raw.getTime()) ? null : { "$date": raw.toISOString() };
      if (raw instanceof RegExp) return { "$regularExpression": { pattern: raw.source, options: raw.flags.replace(/[^imsx]/g, "") } };
      return x;
    }, ind);
  };

  g.ObjectId = function (h) { return { "$oid": h === undefined ? __oid() : String(h) }; };
  g.ISODate = function (s) { return s === undefined ? new Date() : new Date(s); };
  g.NumberLong = function (n) { return { "$numberLong": String(n) }; };
  g.NumberInt = function (n) { return { "$numberInt": String(Math.trunc(Number(n))) }; };
  g.NumberDecimal = function (s) { return { "$numberDecimal": String(s) }; };
  g.UUID = function (s) { return { "$uuid": String(s) }; };
  g.BinData = function (t, b) { return { "$binary": { base64: String(b), subType: ("0" + Number(t).toString(16)).slice(-2) } }; };
  g.Timestamp = function (t, i) { return { "$timestamp": { t: Number(t), i: Number(i) } }; };
  g.MinKey = function () { return { "$minKey": 1 }; };
  g.MaxKey = function () { return { "$maxKey": 1 }; };
})(this);
