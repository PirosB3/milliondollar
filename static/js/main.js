var Tile = React.createClass({
    lock: function() {
        var self = this;
        $.post("/tile", JSON.stringify({
            "frame_number": this.props.idx
        }), function(res) {
            console.log(res);
            self.props.onLock();
        });
    },
    getInitialState: function() {
        return {
            message: ""
        };
    },
    purchase: function(event) {
        if (this.state.message.length == 0) {
            return;
        }
        var self = this;
        $.post("/purchase", JSON.stringify({
            "frame_number": this.props.idx,
            "message": this.state.message
        }), function(res) {
            self.setState({message: ""});
            self.props.onLock();
        });
    },
    onChange: function(event) {
        this.setState({message: event.target.value});
    },
    dataStates: {
        "LOCKED_BY_CURRENT_USER": "Locked by current user",
        "LOCKED_BY_OTHER": "Locked by other",
        "PURCHASED": "Purchased",
        "OPEN": "Open"
    },
    dataClasses: {
        "LOCKED_BY_CURRENT_USER": "label label-default",
        "LOCKED_BY_OTHER": "label label-warning",
        "PURCHASED": "label label-success",
        "OPEN": "label label-info"
    },
    render: function() {
        var src = "https://chart.googleapis.com/chart?chs=250x250&cht=qr&chl=" + this.props.address.address;
        var additional;

        return (
            <div className="col-md-4">
                <div className="panel panel-default tile">
                  <div className="panel-body">
                    <h3 className="text-center buy-btn">Buy</h3>
                    <img className="center-block" src={src} />
                    <div className="row">
                      <div className="form-group">
                        <div className="col-md-9">
                            <input type="text" className="form-control" placeholder="Insert your message here" />
                        </div>
                        <div className="col-md-3 tile-submit-field">
                            <button className="btn btn-default" type="submit">Submit</button>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
            </div>
        );
    }
});

var Balance = React.createClass({
    render: function() {
        return (
            <p>Balance: {this.props.value} BTC deposited</p>
        );
    }
});

var MainComponent = React.createClass({
  getInitialState: function() {
      return { addresses: [], tiles: [], balance: null };
  },
  reloadAddresses: function() {
      var self = this;
      var addressesRequest = $.getJSON('/addresses');
      var tilesRequest = $.getJSON('/tiles');
      $.when(addressesRequest, tilesRequest).then(function(a, b) {
          self.setState({
              addresses: a[0],
              tiles: b[0],
              balance: a[0][0].balance,
          });
      });
  },
  componentDidMount: function() {
      var self = this;
      setInterval(function() {
          self.reloadAddresses();
      }, 3000);
  },
  render: function() {
    var res = [];
    for (var i=0; i < this.state.addresses.length; i++) {
        res.push(
            <Tile key={i} idx={i} data={this.state.tiles[i]} address={this.state.addresses[i]} onLock={this.reloadAddresses} />
        );
    }

    var pairsOfThree = [];
    for (var i = 0; i < res.length; i += 3) {
        var els = res.slice(i, i+3);
        pairsOfThree.push(
            <div className="row">
                {els}
            </div>
        );
    }

    return (
      <div>
          <div className="row">
            <Balance value={this.state.balance} />
          </div>
          <div className="row">
            {pairsOfThree}
          </div>
      </div>
    );
  }
});

ReactDOM.render(
  <MainComponent />,
  document.getElementsByClassName('main')[0]
);
