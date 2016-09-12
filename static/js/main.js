var PurchaseButton = React.createClass({
    render: function() {
        console.log(this.props);
        var classes = ["btn"];
        var buttonText;
        var disabled;
        if (this.props.textLength == 0) {
            classes.push('btn-danger');
            buttonText = "Message is empty";
            disabled = true;
        } else if (this.props.balance == 0) {
            classes.push('btn-danger');
            buttonText = "Insufficient funds";
            disabled = true;
        } else {
            classes.push('btn-default');
            buttonText = "Purchase";
            disabled = false;
        }
        var classString = classes.join(' ');
        return <button disabled={disabled} className={classString} type="submit">{buttonText}</button>;
    }
});

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
        var src = "https://chart.googleapis.com/chart?chs=200x200&cht=qr&chl=" + this.props.address.address;
        var additional;

        var bodyContent;
        var classes = ["col-md-4"];
        console.log(this.props.data.state);
        switch(this.props.data.state) {
        case "OPEN":
            bodyContent = (
              <div className="panel-body">
                <h3 className="text-center buy-btn">For Sale</h3>
                <img className="center-block" src={src} />
                <div className="form-group">
                    <div className="row top-area">
                        <div className="col-md-12">
                            <input value={this.state.message} onChange={this.onChange} type="text" className="form-control" placeholder="Insert your message here" />
                        </div>
                    </div>
                    <div className="row">
                        <div className="col-md-12">
                            <PurchaseButton textLength={this.state.message.length} balance={this.props.balance} />
                        </div>
                    </div>
                </div>
              </div>
            )
            break;
        case "LOCKED_BY_OTHER":
            classes.push('tile-locked');
            bodyContent = (
                <div className="text-center">
                    <h3>Locked</h3>
                    <p>Someone is purchasing this tile</p>
                </div>
            )
            break;
        }

        return (
            <div className={classes.join(' ')}>
                <div className="panel panel-default tile">
                    {bodyContent}
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
            <Tile key={i} balance={this.state.balance} idx={i} data={this.state.tiles[i]} address={this.state.addresses[i]} onLock={this.reloadAddresses} />
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
