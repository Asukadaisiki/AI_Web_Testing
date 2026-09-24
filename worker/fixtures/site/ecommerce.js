(function () {
  "use strict";

  var LOGIN_KEY = "ecommerce-login";
  var CART_KEY = "ecommerce-cart";

  function readJSON(key) {
    try {
      return JSON.parse(window.sessionStorage.getItem(key) || "null");
    } catch (error) {
      return null;
    }
  }

  function writeJSON(key, value) {
    window.sessionStorage.setItem(key, JSON.stringify(value));
  }

  function signIn(email) {
    writeJSON(LOGIN_KEY, { email: email });
  }

  function currentUser() {
    return readJSON(LOGIN_KEY);
  }

  function writeCart(product, quantity) {
    writeJSON(CART_KEY, { product: product, quantity: String(quantity) });
  }

  function readCart() {
    return readJSON(CART_KEY);
  }

  window.EcommerceFixture = {
    currentUser: currentUser,
    readCart: readCart,
    signIn: signIn,
    writeCart: writeCart
  };

  console.log("ecommerce fixture ready");
})();
